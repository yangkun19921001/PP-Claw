package tools

import (
	"context"
	"fmt"

	"github.com/yangkun19921001/PP-Claw/bus"
)

// MessageTool 消息发送工具 (对标 pp-claw/agent/tools/message.py:MessageTool)
type MessageTool struct {
	SendCallback    func(*bus.OutboundMessage)
	SendWithContext func(context.Context, *bus.OutboundMessage)
	channel         string
	accountID       string
	chatID          string
	replyTo         string // 当前会话的 message_id，用于引用回复
	SentInTurn      bool
}

func (t *MessageTool) Name() string { return "message" }
func (t *MessageTool) Description() string {
	return "Send a message to the user. Supports text content and optional media file attachments (images, audio, video, documents). When sending media, the content field serves as the caption — do NOT send a separate follow-up message to confirm delivery."
}
func (t *MessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{"type": "string", "description": "The message content to send"},
			"media": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional: list of file paths to attach (images, audio, video, documents)",
			},
			"channel": map[string]any{"type": "string", "description": "Optional: target channel (telegram, discord, feishu, etc.)"},
			"chat_id": map[string]any{"type": "string", "description": "Optional: target chat/user ID"},
		},
		"required": []any{"content"},
	}
}

func (t *MessageTool) SetContext(channel, chatID string) {
	t.channel = channel
	t.chatID = chatID
}

// SetContextWithAccount 设置带账号维度的上下文
func (t *MessageTool) SetContextWithAccount(channel, accountID, chatID string) {
	t.channel = channel
	t.accountID = accountID
	t.chatID = chatID
}

// SetReplyTo 设置引用回复的消息 ID
func (t *MessageTool) SetReplyTo(replyTo string) {
	t.replyTo = replyTo
}

func (t *MessageTool) StartTurn() {
	t.SentInTurn = false
}

func (t *MessageTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	content, _ := params["content"].(string)
	if content == "" {
		return "", fmt.Errorf("content is required")
	}

	channel, _ := params["channel"].(string)
	chatID, _ := params["chat_id"].(string)
	if channel == "" {
		channel = t.channel
	}
	if chatID == "" {
		chatID = t.chatID
	}

	if channel == "" || chatID == "" {
		return "Error: No target channel/chat specified", nil
	}
	if t.SendWithContext == nil && t.SendCallback == nil {
		return "Error: Message sending not configured", nil
	}

	// 解析 media 参数
	var media []string
	if rawMedia, ok := params["media"]; ok {
		switch m := rawMedia.(type) {
		case []any:
			for _, item := range m {
				if s, ok := item.(string); ok && s != "" {
					media = append(media, s)
				}
			}
		case []string:
			media = m
		}
	}

	msg := bus.NewOutboundMessage(channel, chatID, content)
	msg.AccountID = t.accountID
	msg.Media = media
	msg.ReplyTo = t.replyTo
	if t.SendWithContext != nil {
		t.SendWithContext(ctx, msg)
	} else {
		t.SendCallback(msg)
	}
	t.SentInTurn = true

	if len(media) > 0 {
		return fmt.Sprintf("Message sent to %s:%s with %d attachments", channel, chatID, len(media)), nil
	}
	return fmt.Sprintf("Message sent to %s:%s", channel, chatID), nil
}
