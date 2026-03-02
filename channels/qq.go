package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yangkun19921001/go-nanobot/bus"
	"go.uber.org/zap"
)

// QQChannel QQ 渠道 (对标 channels/qq.py)
type QQChannel struct {
	BaseChannel
	AppID     string
	AppSecret string
	Token     string
	Sandbox   bool

	client  *http.Client
	baseURL string
}

func init() {
	RegisterFactory("qq", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &QQChannel{
			BaseChannel: BaseChannel{
				ChannelName: "qq",
				Bus:         msgBus,
				Logger:      logger,
			},
			baseURL: "https://api.sgroup.qq.com",
		}, nil
	})
}

func (q *QQChannel) Name() string { return "qq" }

// Configure 配置 QQ 渠道
func (q *QQChannel) Configure(appID, appSecret, token string, sandbox bool) {
	q.AppID = appID
	q.AppSecret = appSecret
	q.Token = token
	q.Sandbox = sandbox
	if sandbox {
		q.baseURL = "https://sandbox.api.sgroup.qq.com"
	}
}

// Start 启动 QQ Bot (WebSocket)
func (q *QQChannel) Start(ctx context.Context) error {
	if q.AppID == "" || q.Token == "" {
		return fmt.Errorf("qq app_id and token not configured")
	}

	q.client = &http.Client{Timeout: 30 * time.Second}
	q.Running = true
	q.Logger.Info("QQ 渠道启动", zap.String("app_id", q.AppID))

	// 注: 完整实现应使用 QQ Bot WebSocket Gateway
	<-ctx.Done()
	return nil
}

func (q *QQChannel) Stop() error {
	q.Running = false
	return nil
}

// Send 发送消息到 QQ (对标 qq.py:send)
func (q *QQChannel) Send(msg *bus.OutboundMessage) error {
	if q.client == nil {
		return fmt.Errorf("qq not started")
	}

	payload := map[string]any{
		"content": msg.Content,
	}
	data, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/channels/%s/messages", q.baseURL, msg.ChatID)
	req, _ := http.NewRequest("POST", url, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bot %s.%s", q.AppID, q.Token))

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qq API error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
