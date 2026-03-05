package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yangkun19921001/PP-Claw/bus"
	"go.uber.org/zap"
)

// MochatChannel Mochat 渠道 (对标 channels/mochat.py)
type MochatChannel struct {
	BaseChannel
	BaseURL       string
	PollInterval  int // 秒
	client        *http.Client
	lastMessageID string
}

func init() {
	RegisterFactory("mochat", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &MochatChannel{
			BaseChannel: BaseChannel{
				ChannelName: "mochat",
				Bus:         msgBus,
				Logger:      logger,
			},
			PollInterval: 3,
		}, nil
	})
}

func (m *MochatChannel) Name() string { return "mochat" }

// Configure 配置 Mochat 渠道
func (m *MochatChannel) Configure(baseURL string, allowFrom []string) {
	m.BaseURL = baseURL
	m.AllowFrom = allowFrom
}

// Start 启动 Mochat (HTTP 轮询)
func (m *MochatChannel) Start(ctx context.Context) error {
	if m.BaseURL == "" {
		return fmt.Errorf("mochat base_url not configured")
	}

	m.client = &http.Client{Timeout: 30 * time.Second}
	m.Running = true
	m.Logger.Info("Mochat 渠道启动", zap.String("base_url", m.BaseURL))

	ticker := time.NewTicker(time.Duration(m.PollInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			m.pollMessages()
		}
	}
}

func (m *MochatChannel) Stop() error {
	m.Running = false
	return nil
}

// pollMessages 轮询新消息
func (m *MochatChannel) pollMessages() {
	url := fmt.Sprintf("%s/api/messages?after=%s", m.BaseURL, m.lastMessageID)
	resp, err := m.client.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var messages []struct {
		ID      string `json:"id"`
		Sender  string `json:"sender"`
		Content string `json:"content"`
	}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &messages)

	for _, msg := range messages {
		m.lastMessageID = msg.ID
		m.HandleMessage(msg.Sender, msg.Sender, msg.Content, nil, nil)
	}
}

// Send 发送消息到 Mochat (对标 mochat.py:send)
func (m *MochatChannel) Send(msg *bus.OutboundMessage) error {
	if m.client == nil {
		return fmt.Errorf("mochat not started")
	}

	payload := map[string]any{
		"to":      msg.ChatID,
		"content": msg.Content,
	}
	data, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/api/messages/send", m.BaseURL)
	resp, err := m.client.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mochat send failed %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
