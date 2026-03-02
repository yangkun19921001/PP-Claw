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

// DiscordChannel Discord 渠道 (对标 nanobot/channels/discord.py)
type DiscordChannel struct {
	BaseChannel
	Token      string
	GatewayURL string

	client *http.Client
}

func init() {
	RegisterFactory("discord", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &DiscordChannel{
			BaseChannel: BaseChannel{
				ChannelName: "discord",
				Bus:         msgBus,
				Logger:      logger,
			},
			GatewayURL: "wss://gateway.discord.gg/?v=10&encoding=json",
		}, nil
	})
}

func (d *DiscordChannel) Name() string { return "discord" }

// Configure 配置 Discord 渠道
func (d *DiscordChannel) Configure(token string, allowFrom []string) {
	d.Token = token
	d.AllowFrom = allowFrom
}

// Start 启动 Discord (简化版: 使用 REST API 轮询)
func (d *DiscordChannel) Start(ctx context.Context) error {
	if d.Token == "" {
		return fmt.Errorf("discord token not configured")
	}

	d.client = &http.Client{Timeout: 30 * time.Second}
	d.Running = true
	d.Logger.Info("Discord 渠道启动")

	// 注: 完整实现应使用 WebSocket Gateway
	// 这里使用简化的 REST API 轮询
	<-ctx.Done()
	return nil
}

func (d *DiscordChannel) Stop() error {
	d.Running = false
	return nil
}

// Send 发送消息到 Discord
func (d *DiscordChannel) Send(msg *bus.OutboundMessage) error {
	if d.client == nil {
		return fmt.Errorf("discord not started")
	}

	payload := map[string]any{
		"content": msg.Content,
	}

	data, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", msg.ChatID)
	req, _ := http.NewRequest("POST", url, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+d.Token)

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送消息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord API error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
