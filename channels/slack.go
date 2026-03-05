package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/yangkun19921001/PP-Claw/bus"
	"go.uber.org/zap"
)

// SlackChannel Slack 渠道 (对标 channels/slack.py - 282行)
type SlackChannel struct {
	BaseChannel
	BotToken       string
	AppToken       string
	Mode           string // "socket"
	ReplyInThread  bool
	ReactEmoji     string
	GroupPolicy    string // "open", "mention", "allowlist"
	GroupAllowFrom []string
	DMEnabled      bool
	DMPolicy       string
	DMAllowFrom    []string

	client    *http.Client
	botUserID string
}

func init() {
	RegisterFactory("slack", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &SlackChannel{
			BaseChannel: BaseChannel{
				ChannelName: "slack",
				Bus:         msgBus,
				Logger:      logger,
			},
			Mode:       "socket",
			ReactEmoji: "eyes",
			DMEnabled:  true,
			DMPolicy:   "open",
		}, nil
	})
}

func (s *SlackChannel) Name() string { return "slack" }

// Configure 配置 Slack 渠道
func (s *SlackChannel) Configure(botToken, appToken string, allowFrom []string) {
	s.BotToken = botToken
	s.AppToken = appToken
	s.AllowFrom = allowFrom
}

// Start 启动 Slack Socket Mode
func (s *SlackChannel) Start(ctx context.Context) error {
	if s.BotToken == "" || s.AppToken == "" {
		return fmt.Errorf("slack bot_token and app_token not configured")
	}

	s.client = &http.Client{Timeout: 30 * time.Second}
	s.Running = true
	s.Logger.Info("Slack 渠道启动 (Socket Mode)")

	// 获取 bot user ID
	s.resolveBotUserID()

	// 注: 完整实现应使用 slack-sdk 的 Socket Mode
	<-ctx.Done()
	return nil
}

func (s *SlackChannel) Stop() error {
	s.Running = false
	return nil
}

// resolveBotUserID 获取 bot user ID
func (s *SlackChannel) resolveBotUserID() {
	req, _ := http.NewRequest("POST", "https://slack.com/api/auth.test", nil)
	req.Header.Set("Authorization", "Bearer "+s.BotToken)

	resp, err := s.client.Do(req)
	if err != nil {
		s.Logger.Warn("Slack auth.test 失败", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool   `json:"ok"`
		UserID string `json:"user_id"`
	}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &result)

	if result.OK {
		s.botUserID = result.UserID
		s.Logger.Info("Slack bot 已连接", zap.String("user_id", s.botUserID))
	}
}

// Send 发送消息到 Slack (对标 slack.py:send)
func (s *SlackChannel) Send(msg *bus.OutboundMessage) error {
	if s.client == nil {
		return fmt.Errorf("slack not started")
	}

	// 获取线程信息
	var threadTS string
	if slackMeta, ok := msg.Metadata["slack"].(map[string]any); ok {
		if ts, ok := slackMeta["thread_ts"].(string); ok {
			channelType, _ := slackMeta["channel_type"].(string)
			if channelType != "im" { // DM 不使用线程
				threadTS = ts
			}
		}
	}

	if msg.Content != "" {
		content := s.toMrkdwn(msg.Content)
		return s.postMessage(msg.ChatID, content, threadTS)
	}

	return nil
}

// postMessage 发送消息
func (s *SlackChannel) postMessage(channel, text, threadTS string) error {
	payload := map[string]any{
		"channel": channel,
		"text":    text,
	}
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}

	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://slack.com/api/chat.postMessage",
		strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.BotToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &result)

	if !result.OK {
		return fmt.Errorf("slack API error: %s", result.Error)
	}
	return nil
}

// toMrkdwn 转换 Markdown 到 Slack mrkdwn (对标 slack.py:_to_mrkdwn)
func (s *SlackChannel) toMrkdwn(text string) string {
	if text == "" {
		return ""
	}
	// 转换标题
	text = headingToSlack(text)
	// 转换粗体
	text = strings.ReplaceAll(text, "**", "*")
	return text
}

var slackHeadingRE = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)

func headingToSlack(text string) string {
	return slackHeadingRE.ReplaceAllString(text, "*$1*")
}
