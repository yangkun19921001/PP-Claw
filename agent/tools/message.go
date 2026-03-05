package tools

import (
	"context"
	"fmt"

	"github.com/yangkun19921001/PP-Claw/bus"
)

// MessageTool 消息发送工具 (对标 pp-claw/agent/tools/message.py:MessageTool)
type MessageTool struct {
	SendCallback func(*bus.OutboundMessage)
	channel      string
	chatID       string
	SentInTurn   bool
}

func (t *MessageTool) Name() string { return "message" }
func (t *MessageTool) Description() string {
	return "Send a message to the user. Use this when you want to communicate something."
}
func (t *MessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{"type": "string", "description": "The message content to send"},
			"channel": map[string]any{"type": "string", "description": "Optional: target channel (telegram, discord, etc.)"},
			"chat_id": map[string]any{"type": "string", "description": "Optional: target chat/user ID"},
		},
		"required": []any{"content"},
	}
}

func (t *MessageTool) SetContext(channel, chatID string) {
	t.channel = channel
	t.chatID = chatID
}

func (t *MessageTool) StartTurn() {
	t.SentInTurn = false
}

func (t *MessageTool) Execute(_ context.Context, params map[string]any) (string, error) {
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
	if t.SendCallback == nil {
		return "Error: Message sending not configured", nil
	}

	msg := bus.NewOutboundMessage(channel, chatID, content)
	t.SendCallback(msg)
	t.SentInTurn = true

	return fmt.Sprintf("Message sent to %s:%s", channel, chatID), nil
}
