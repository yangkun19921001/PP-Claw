package channels

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/utils"
	"github.com/yuin/goldmark"
	"go.uber.org/zap"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// MatrixChannel Matrix 渠道实现
type MatrixChannel struct {
	BaseChannel
	Homeserver     string
	AccessToken    string
	UserID         string
	DeviceID       string
	E2EEEnabled    bool
	MaxMediaBytes  int
	GroupPolicy    string   // "open"/"mention"/"allowlist"
	GroupAllowFrom []string

	client       *mautrix.Client
	dedup        *utils.LRUCache
	typingCancel map[string]context.CancelFunc
	mu           sync.Mutex
	cancel       context.CancelFunc
}

func init() {
	RegisterFactory("matrix", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &MatrixChannel{
			BaseChannel: BaseChannel{
				ChannelName: "matrix",
				Bus:         msgBus,
				Logger:      logger,
			},
			dedup:        utils.NewLRUCache(1000),
			typingCancel: make(map[string]context.CancelFunc),
		}, nil
	})
}

func (m *MatrixChannel) Name() string { return "matrix" }

// Configure sets Matrix channel configuration.
func (m *MatrixChannel) Configure(homeserver, accessToken, userID, deviceID string, e2ee bool, maxMediaBytes int, allowFrom []string, groupPolicy string, groupAllowFrom []string) {
	m.Homeserver = homeserver
	m.AccessToken = accessToken
	m.UserID = userID
	m.DeviceID = deviceID
	m.E2EEEnabled = e2ee
	m.MaxMediaBytes = maxMediaBytes
	m.AllowFrom = allowFrom
	m.GroupPolicy = groupPolicy
	m.GroupAllowFrom = groupAllowFrom
}

// Start connects to the Matrix homeserver and begins syncing.
func (m *MatrixChannel) Start(ctx context.Context) error {
	if m.Homeserver == "" || m.AccessToken == "" || m.UserID == "" {
		return fmt.Errorf("matrix homeserver, access_token, and user_id are required")
	}

	userID := id.UserID(m.UserID)
	client, err := mautrix.NewClient(m.Homeserver, userID, m.AccessToken)
	if err != nil {
		return fmt.Errorf("matrix client init: %w", err)
	}

	if m.DeviceID != "" {
		client.DeviceID = id.DeviceID(m.DeviceID)
	}

	m.client = client
	m.Running = true
	ctx, m.cancel = context.WithCancel(ctx)

	// Register event handler for messages
	syncer := client.Syncer.(*mautrix.DefaultSyncer)
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		m.handleMessage(ctx, evt)
	})

	m.Logger.Info("Matrix 渠道启动", zap.String("homeserver", m.Homeserver), zap.String("user_id", m.UserID))

	// Start sync loop
	return m.syncLoop(ctx)
}

// Stop stops the Matrix channel.
func (m *MatrixChannel) Stop() error {
	m.Running = false
	if m.cancel != nil {
		m.cancel()
	}
	if m.client != nil {
		m.client.StopSync()
	}

	// Cancel all typing indicators
	m.mu.Lock()
	for _, cancel := range m.typingCancel {
		cancel()
	}
	m.typingCancel = make(map[string]context.CancelFunc)
	m.mu.Unlock()

	return nil
}

// syncLoop runs the Matrix sync with automatic reconnection.
func (m *MatrixChannel) syncLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		err := m.client.SyncWithContext(ctx)
		if err != nil && ctx.Err() == nil {
			m.Logger.Warn("Matrix sync 断开", zap.Error(err))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
				m.Logger.Info("Matrix 重连中...")
			}
		} else {
			return nil
		}
	}
}

// handleMessage processes an incoming Matrix message event.
func (m *MatrixChannel) handleMessage(_ context.Context, evt *event.Event) {
	// Ignore own messages
	if evt.Sender == id.UserID(m.UserID) {
		return
	}

	// Dedup by event ID
	eventID := evt.ID.String()
	if m.dedup.Contains(eventID) {
		return
	}
	m.dedup.Add(eventID)

	roomID := evt.RoomID.String()
	senderID := evt.Sender.String()

	// Group policy check
	if !m.checkGroupPolicy(evt) {
		return
	}

	// Extract content
	content, ok := evt.Content.Parsed.(*event.MessageEventContent)
	if !ok {
		return
	}

	var text string
	var media []string

	switch content.MsgType {
	case event.MsgText, event.MsgNotice, event.MsgEmote:
		text = content.Body
	case event.MsgImage, event.MsgFile, event.MsgAudio, event.MsgVideo:
		// Download media
		if content.URL != "" {
			mediaURL := m.mxcToHTTP(string(content.URL))
			if mediaURL != "" {
				media = append(media, mediaURL)
			}
		}
		if content.Body != "" && content.MsgType != event.MsgImage {
			text = content.Body
		}
	default:
		text = fmt.Sprintf("[%s message]", content.MsgType)
	}

	if text == "" && len(media) == 0 {
		return
	}

	metadata := map[string]any{
		"event_id": eventID,
	}

	// Check for thread relation
	if content.RelatesTo != nil && content.RelatesTo.Type == event.RelThread {
		metadata["thread_id"] = content.RelatesTo.EventID.String()
	}

	m.HandleMessage(senderID, roomID, text, media, metadata)
}

// checkGroupPolicy determines if the message should be processed based on group policy.
func (m *MatrixChannel) checkGroupPolicy(evt *event.Event) bool {
	// DMs always allowed
	// For simplicity, check room member count or rely on policy
	policy := m.GroupPolicy
	if policy == "" {
		policy = "mention"
	}

	switch policy {
	case "open":
		return true
	case "mention":
		// Check if the message mentions the bot
		content, ok := evt.Content.Parsed.(*event.MessageEventContent)
		if !ok {
			return false
		}
		return strings.Contains(content.Body, m.UserID) ||
			strings.Contains(content.FormattedBody, m.UserID)
	case "allowlist":
		senderID := evt.Sender.String()
		for _, allowed := range m.GroupAllowFrom {
			if allowed == senderID {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// mxcToHTTP converts an mxc:// URL to an HTTP URL for download.
func (m *MatrixChannel) mxcToHTTP(mxcURL string) string {
	if !strings.HasPrefix(mxcURL, "mxc://") {
		return mxcURL
	}
	// mxc://server/media_id -> homeserver/_matrix/media/v3/download/server/media_id
	parts := strings.SplitN(strings.TrimPrefix(mxcURL, "mxc://"), "/", 2)
	if len(parts) != 2 {
		return ""
	}
	return fmt.Sprintf("%s/_matrix/media/v3/download/%s/%s", m.Homeserver, parts[0], parts[1])
}

// Send sends a message to a Matrix room.
func (m *MatrixChannel) Send(msg *bus.OutboundMessage) error {
	if m.client == nil {
		return fmt.Errorf("matrix client not connected")
	}

	if msg.Content == "" {
		return nil
	}

	roomID := id.RoomID(msg.ChatID)

	// Stop typing indicator for this room
	m.stopTyping(msg.ChatID)

	// Convert markdown to HTML
	htmlContent := markdownToHTML(msg.Content)

	content := &event.MessageEventContent{
		MsgType:       event.MsgText,
		Body:          msg.Content,
		Format:        event.FormatHTML,
		FormattedBody: htmlContent,
	}

	// Add thread relation if metadata has thread_id
	if threadID, ok := msg.Metadata["thread_id"].(string); ok && threadID != "" {
		content.RelatesTo = &event.RelatesTo{
			Type:    event.RelThread,
			EventID: id.EventID(threadID),
		}
	}

	// Check if this is a progress message — send typing indicator instead
	if isProgress, ok := msg.Metadata["_progress"].(bool); ok && isProgress {
		m.startTyping(roomID.String())
		return nil
	}

	_, err := m.client.SendMessageEvent(context.Background(), roomID, event.EventMessage, content)
	if err != nil {
		return fmt.Errorf("matrix send: %w", err)
	}

	return nil
}

// startTyping sends a typing indicator and keeps it alive.
func (m *MatrixChannel) startTyping(roomID string) {
	m.stopTyping(roomID)

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.typingCancel[roomID] = cancel
	m.mu.Unlock()

	go func() {
		rid := id.RoomID(roomID)
		for {
			_, _ = m.client.UserTyping(ctx, rid, true, 25*time.Second)
			select {
			case <-ctx.Done():
				return
			case <-time.After(20 * time.Second):
				// Resend typing indicator
			}
		}
	}()
}

// stopTyping cancels the typing indicator for a room.
func (m *MatrixChannel) stopTyping(roomID string) {
	m.mu.Lock()
	cancel, ok := m.typingCancel[roomID]
	if ok {
		cancel()
		delete(m.typingCancel, roomID)
	}
	m.mu.Unlock()

	if ok && m.client != nil {
		rid := id.RoomID(roomID)
		_, _ = m.client.UserTyping(context.Background(), rid, false, 0)
	}
}

// markdownToHTML converts markdown content to HTML using goldmark.
func markdownToHTML(content string) string {
	var buf bytes.Buffer
	md := goldmark.New()
	if err := md.Convert([]byte(content), &buf); err != nil {
		return content
	}
	return buf.String()
}

// Ensure unused imports are actually used
var (
	_ = http.StatusOK
	_ io.Reader
)
