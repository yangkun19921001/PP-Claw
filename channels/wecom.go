package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/utils"
	"go.uber.org/zap"
)

// WecomChannel 企业微信渠道 — WebSocket 实现
type WecomChannel struct {
	BaseChannel
	BotID          string
	Secret         string
	WelcomeMessage string

	wsConn     *websocket.Conn
	dedup      *utils.LRUCache
	chatFrames map[string]json.RawMessage
	mu         sync.Mutex
	cancel     context.CancelFunc
}

func init() {
	RegisterFactory("wecom", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &WecomChannel{
			BaseChannel: BaseChannel{
				ChannelName: "wecom",
				Bus:         msgBus,
				Logger:      logger,
			},
			dedup:      utils.NewLRUCache(1000),
			chatFrames: make(map[string]json.RawMessage),
		}, nil
	})
}

func (w *WecomChannel) Name() string { return "wecom" }

// Configure sets WeCom channel configuration.
func (w *WecomChannel) Configure(botID, secret, welcomeMessage string, allowFrom []string) {
	w.BotID = botID
	w.Secret = secret
	w.WelcomeMessage = welcomeMessage
	w.AllowFrom = allowFrom
}

// Start connects to the WeCom WebSocket gateway.
func (w *WecomChannel) Start(ctx context.Context) error {
	if w.BotID == "" || w.Secret == "" {
		return fmt.Errorf("wecom bot_id and secret not configured")
	}

	w.Running = true
	ctx, w.cancel = context.WithCancel(ctx)
	w.Logger.Info("企业微信渠道启动 (WebSocket)")

	return w.connectLoop(ctx)
}

// Stop stops the WeCom channel.
func (w *WecomChannel) Stop() error {
	w.Running = false
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Lock()
	if w.wsConn != nil {
		w.wsConn.Close()
	}
	w.mu.Unlock()
	return nil
}

// connectLoop handles WebSocket connection with automatic reconnection.
func (w *WecomChannel) connectLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		err := w.connectAndRead(ctx)
		if err != nil {
			w.Logger.Warn("WeCom WebSocket 断开", zap.Error(err))
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
			w.Logger.Info("WeCom WebSocket 重连中...")
		}
	}
}

// connectAndRead establishes a WebSocket connection and reads messages.
func (w *WecomChannel) connectAndRead(ctx context.Context) error {
	wsURL := fmt.Sprintf("wss://bot.wecom.work/ws?bot_id=%s&secret=%s", w.BotID, w.Secret)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	w.mu.Lock()
	w.wsConn = conn
	w.mu.Unlock()

	defer func() {
		conn.Close()
		w.mu.Lock()
		w.wsConn = nil
		w.mu.Unlock()
	}()

	w.Logger.Info("WeCom WebSocket 已连接")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, msgData, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		w.handleMessage(ctx, msgData)
	}
}

// handleMessage processes an incoming WeCom WebSocket message.
func (w *WecomChannel) handleMessage(_ context.Context, data []byte) {
	var msg struct {
		Type    string `json:"type"`
		Data    struct {
			MsgID    string `json:"msg_id"`
			ChatID   string `json:"chat_id"`
			ChatType string `json:"chat_type"`
			SenderID string `json:"sender_id"`
			MsgType  string `json:"msg_type"`
			Content  struct {
				Text    string `json:"text"`
				ImageURL string `json:"image_url"`
				FileURL  string `json:"file_url"`
			} `json:"content"`
		} `json:"data"`
	}

	if err := json.Unmarshal(data, &msg); err != nil {
		w.Logger.Debug("WeCom 消息解析失败", zap.Error(err))
		return
	}

	switch msg.Type {
	case "enter_chat":
		// Welcome message
		if w.WelcomeMessage != "" && msg.Data.ChatID != "" {
			out := bus.NewOutboundMessage("wecom", msg.Data.ChatID, w.WelcomeMessage)
			w.Bus.PublishOutbound(out)
		}
		return
	case "message":
		// Continue processing
	default:
		return
	}

	// Dedup
	if msg.Data.MsgID != "" && w.dedup.Contains(msg.Data.MsgID) {
		return
	}
	if msg.Data.MsgID != "" {
		w.dedup.Add(msg.Data.MsgID)
	}

	// Extract content
	var content string
	var media []string

	switch msg.Data.MsgType {
	case "text":
		content = msg.Data.Content.Text
	case "image":
		if msg.Data.Content.ImageURL != "" {
			media = append(media, msg.Data.Content.ImageURL)
		}
	case "file":
		if msg.Data.Content.FileURL != "" {
			media = append(media, msg.Data.Content.FileURL)
		}
	case "mixed":
		content = msg.Data.Content.Text
		if msg.Data.Content.ImageURL != "" {
			media = append(media, msg.Data.Content.ImageURL)
		}
	default:
		content = fmt.Sprintf("[%s message]", msg.Data.MsgType)
	}

	if content == "" && len(media) == 0 {
		return
	}

	metadata := map[string]any{
		"message_id": msg.Data.MsgID,
		"chat_type":  msg.Data.ChatType,
	}

	w.HandleMessage(msg.Data.SenderID, msg.Data.ChatID, content, media, metadata)
}

// Send sends a message to a WeCom chat via WebSocket.
func (w *WecomChannel) Send(msg *bus.OutboundMessage) error {
	w.mu.Lock()
	conn := w.wsConn
	w.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("wecom websocket not connected")
	}

	if msg.Content == "" {
		return nil
	}

	// Stream reply format
	frame := map[string]any{
		"type": "reply_stream",
		"data": map[string]any{
			"chat_id": msg.ChatID,
			"content": map[string]any{
				"text": msg.Content,
			},
		},
	}

	data, _ := json.Marshal(frame)

	w.mu.Lock()
	err := conn.WriteMessage(websocket.TextMessage, data)
	w.mu.Unlock()

	if err != nil {
		return fmt.Errorf("wecom send: %w", err)
	}

	return nil
}

// Ensure unused imports are actually used
var _ = strings.TrimSpace
