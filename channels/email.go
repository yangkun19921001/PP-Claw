package channels

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yangkun19921001/PP-Claw/bus"
	"go.uber.org/zap"
)

// EmailChannel 邮件渠道 (对标 channels/email.py)
type EmailChannel struct {
	BaseChannel
	IMAPHost      string
	IMAPPort      int
	SMTPHost      string
	SMTPPort      int
	Username      string
	Password      string
	CheckInterval int // 秒
}

func init() {
	RegisterFactory("email", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &EmailChannel{
			BaseChannel: BaseChannel{
				ChannelName: "email",
				Bus:         msgBus,
				Logger:      logger,
			},
			IMAPPort:      993,
			SMTPPort:      587,
			CheckInterval: 60,
		}, nil
	})
}

func (e *EmailChannel) Name() string { return "email" }

// Configure 配置邮件渠道
func (e *EmailChannel) Configure(imapHost string, imapPort int, smtpHost string, smtpPort int, username, password string) {
	e.IMAPHost = imapHost
	e.IMAPPort = imapPort
	e.SMTPHost = smtpHost
	e.SMTPPort = smtpPort
	e.Username = username
	e.Password = password
}

// Start 启动邮件渠道 (IMAP 轮询)
func (e *EmailChannel) Start(ctx context.Context) error {
	if e.IMAPHost == "" || e.Username == "" {
		return fmt.Errorf("email IMAP not configured")
	}

	e.Running = true
	e.Logger.Info("邮件渠道启动",
		zap.String("imap", e.IMAPHost),
		zap.String("smtp", e.SMTPHost),
	)

	// IMAP 轮询循环
	ticker := time.NewTicker(time.Duration(e.CheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			e.checkNewMail()
		}
	}
}

func (e *EmailChannel) Stop() error {
	e.Running = false
	return nil
}

// checkNewMail 检查新邮件
func (e *EmailChannel) checkNewMail() {
	// 注: 完整实现应使用 go-imap 库
	e.Logger.Debug("检查新邮件...")
}

// Send 发送邮件 (对标 email.py:send)
func (e *EmailChannel) Send(msg *bus.OutboundMessage) error {
	if e.SMTPHost == "" {
		return fmt.Errorf("email SMTP not configured")
	}

	// 注: 完整实现应使用 net/smtp
	_ = msg
	e.Logger.Info("发送邮件", zap.String("to", msg.ChatID))

	// 构建邮件
	subject := "pp-claw"
	body := msg.Content
	if len(body) > 100 {
		subject = body[:100] + "..."
	}
	_ = subject
	_ = strings.NewReader(body)

	return nil
}
