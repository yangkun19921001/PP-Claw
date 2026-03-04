package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yangkun19921001/go-nanobot/bus"
	"go.uber.org/zap"
)

// TelegramChannel Telegram 渠道 (对标 nanobot/channels/telegram.py:TelegramChannel)
type TelegramChannel struct {
	BaseChannel
	Token string
	Proxy string

	client  *http.Client
	baseURL string
	offset  int
}

func init() {
	RegisterFactory("telegram", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &TelegramChannel{
			BaseChannel: BaseChannel{
				ChannelName: "telegram",
				Bus:         msgBus,
				Logger:      logger,
			},
		}, nil
	})
}

func (t *TelegramChannel) Name() string { return "telegram" }

// Configure 配置 Telegram 渠道
func (t *TelegramChannel) Configure(token string, allowFrom []string, proxy string) {
	t.Token = token
	t.AllowFrom = allowFrom
	t.Proxy = proxy
	t.baseURL = fmt.Sprintf("https://api.telegram.org/bot%s", token)
}

// Start 启动 Telegram 长轮询
func (t *TelegramChannel) Start(ctx context.Context) error {
	if t.Token == "" {
		return fmt.Errorf("telegram token not configured")
	}

	// 设置 HTTP 客户端 (支持代理)
	transport := &http.Transport{}
	if t.Proxy != "" {
		proxyURL, err := url.Parse(t.Proxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	t.client = &http.Client{Transport: transport, Timeout: 60 * time.Second}
	t.Running = true

	t.Logger.Info("Telegram 渠道启动", zap.String("bot_url", t.baseURL[:40]+"..."))

	// 长轮询循环
	for t.Running {
		select {
		case <-ctx.Done():
			return nil
		default:
			updates, err := t.getUpdates(ctx)
			if err != nil {
				t.Logger.Error("获取 Telegram 更新失败", zap.Error(err))
				time.Sleep(5 * time.Second)
				continue
			}
			for _, update := range updates {
				t.processUpdate(update)
			}
		}
	}
	return nil
}

func (t *TelegramChannel) Stop() error {
	t.Running = false
	return nil
}

// Send 发送消息到 Telegram (对标 telegram.py:send)
func (t *TelegramChannel) Send(msg *bus.OutboundMessage) error {
	if t.client == nil {
		return fmt.Errorf("telegram not started")
	}

	payload := map[string]any{
		"chat_id":    msg.ChatID,
		"text":       msg.Content,
		"parse_mode": "Markdown",
	}

	// 引用回复: Telegram message_id 是整数
	if msg.ReplyTo != "" {
		if replyID, err := strconv.Atoi(msg.ReplyTo); err == nil {
			payload["reply_to_message_id"] = replyID
		}
	}

	data, _ := json.Marshal(payload)
	resp, err := t.client.Post(
		t.baseURL+"/sendMessage",
		"application/json",
		strings.NewReader(string(data)),
	)
	if err != nil {
		return fmt.Errorf("发送消息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// telegramUpdate Telegram 更新结构
type telegramUpdate struct {
	UpdateID int `json:"update_id"`
	Message  *struct {
		MessageID int `json:"message_id"`
		From      *struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
		} `json:"from"`
		Chat *struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

// getUpdates 长轮询获取更新
func (t *TelegramChannel) getUpdates(ctx context.Context) ([]telegramUpdate, error) {
	reqURL := fmt.Sprintf("%s/getUpdates?offset=%d&timeout=30", t.baseURL, t.offset)

	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool             `json:"ok"`
		Result []telegramUpdate `json:"result"`
	}

	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if !result.OK {
		return nil, fmt.Errorf("telegram API returned ok=false")
	}

	return result.Result, nil
}

// processUpdate 处理单个更新
func (t *TelegramChannel) processUpdate(update telegramUpdate) {
	t.offset = update.UpdateID + 1

	if update.Message == nil || update.Message.Text == "" {
		return
	}

	msg := update.Message
	senderID := ""
	if msg.From != nil {
		senderID = fmt.Sprintf("%d", msg.From.ID)
	}
	chatID := fmt.Sprintf("%d", msg.Chat.ID)

	t.HandleMessage(senderID, chatID, msg.Text, nil, map[string]any{
		"message_id": msg.MessageID,
	})
}
