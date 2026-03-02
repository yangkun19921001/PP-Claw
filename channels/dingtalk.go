package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yangkun19921001/go-nanobot/bus"
	"go.uber.org/zap"
)

// DingTalkChannel 钉钉渠道 (对标 channels/dingtalk.py - 248行)
type DingTalkChannel struct {
	BaseChannel
	ClientID     string
	ClientSecret string

	client      *http.Client
	accessToken string
	tokenExpiry time.Time
	mu          sync.Mutex
}

func init() {
	RegisterFactory("dingtalk", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &DingTalkChannel{
			BaseChannel: BaseChannel{
				ChannelName: "dingtalk",
				Bus:         msgBus,
				Logger:      logger,
			},
		}, nil
	})
}

func (d *DingTalkChannel) Name() string { return "dingtalk" }

// Configure 配置钉钉渠道
func (d *DingTalkChannel) Configure(clientID, clientSecret string, allowFrom []string) {
	d.ClientID = clientID
	d.ClientSecret = clientSecret
	d.AllowFrom = allowFrom
}

// Start 启动钉钉 (Stream Mode) (对标 dingtalk.py:start)
func (d *DingTalkChannel) Start(ctx context.Context) error {
	if d.ClientID == "" || d.ClientSecret == "" {
		return fmt.Errorf("dingtalk client_id and client_secret not configured")
	}

	d.client = &http.Client{Timeout: 30 * time.Second}
	d.Running = true
	d.Logger.Info("钉钉渠道启动 (Stream Mode)")

	// 获取 access token
	if err := d.refreshAccessToken(); err != nil {
		d.Logger.Error("获取钉钉 access token 失败", zap.Error(err))
	}

	// 注: 完整实现应使用 dingtalk-stream SDK
	<-ctx.Done()
	return nil
}

func (d *DingTalkChannel) Stop() error {
	d.Running = false
	return nil
}

// refreshAccessToken 刷新 access token (对标 dingtalk.py:_get_access_token)
func (d *DingTalkChannel) refreshAccessToken() error {
	payload := map[string]string{
		"appKey":    d.ClientID,
		"appSecret": d.ClientSecret,
	}
	data, _ := json.Marshal(payload)

	resp, err := d.client.Post(
		"https://api.dingtalk.com/v1.0/oauth2/accessToken",
		"application/json",
		strings.NewReader(string(data)),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int    `json:"expireIn"`
	}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &result)

	if result.AccessToken == "" {
		return fmt.Errorf("dingtalk auth failed")
	}

	d.mu.Lock()
	d.accessToken = result.AccessToken
	d.tokenExpiry = time.Now().Add(time.Duration(result.ExpireIn-60) * time.Second)
	d.mu.Unlock()

	return nil
}

// Send 发送消息到钉钉 (对标 dingtalk.py:send)
func (d *DingTalkChannel) Send(msg *bus.OutboundMessage) error {
	d.mu.Lock()
	if time.Now().After(d.tokenExpiry) {
		d.refreshAccessToken()
	}
	token := d.accessToken
	d.mu.Unlock()

	if token == "" {
		return fmt.Errorf("dingtalk access token not available")
	}

	// 使用 robot/oToMessages/batchSend API
	payload := map[string]any{
		"robotCode": d.ClientID,
		"userIds":   []string{msg.ChatID},
		"msgKey":    "sampleMarkdown",
		"msgParam":  fmt.Sprintf(`{"title":"nanobot","text":"%s"}`, escapeJSON(msg.Content)),
	}

	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST",
		"https://api.dingtalk.com/v1.0/robot/oToMessages/batchSend",
		strings.NewReader(string(data)),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dingtalk send failed %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// escapeJSON 转义 JSON 字符串中的特殊字符
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
