package bus

import "time"

// InboundMessage 从渠道收到的消息 (对标 pp-claw/bus/events.py:InboundMessage)
type InboundMessage struct {
	Channel            string         `json:"channel"`                        // telegram, discord, slack, cli...
	SenderID           string         `json:"sender_id"`                      // 用户标识
	ChatID             string         `json:"chat_id"`                        // 会话标识
	Content            string         `json:"content"`                        // 消息文本
	Timestamp          time.Time      `json:"timestamp"`                      // 时间戳
	Media              []string       `json:"media"`                          // 媒体 URL 列表
	Metadata           map[string]any `json:"metadata"`                       // 渠道特定数据
	SessionKeyOverride string         `json:"session_key_override,omitempty"` // 可选的 session key 覆盖
}

// SessionKey 返回会话唯一标识
func (m *InboundMessage) SessionKey() string {
	if m.SessionKeyOverride != "" {
		return m.SessionKeyOverride
	}
	return m.Channel + ":" + m.ChatID
}

// NewInboundMessage 创建入站消息
func NewInboundMessage(channel, senderID, chatID, content string) *InboundMessage {
	return &InboundMessage{
		Channel:   channel,
		SenderID:  senderID,
		ChatID:    chatID,
		Content:   content,
		Timestamp: time.Now(),
		Media:     []string{},
		Metadata:  map[string]any{},
	}
}

// OutboundMessage 发送到渠道的消息 (对标 pp-claw/bus/events.py:OutboundMessage)
type OutboundMessage struct {
	Channel  string         `json:"channel"`
	ChatID   string         `json:"chat_id"`
	Content  string         `json:"content"`
	ReplyTo  string         `json:"reply_to,omitempty"`
	Media    []string       `json:"media"`
	Metadata map[string]any `json:"metadata"`
}

// NewOutboundMessage 创建出站消息
func NewOutboundMessage(channel, chatID, content string) *OutboundMessage {
	return &OutboundMessage{
		Channel:  channel,
		ChatID:   chatID,
		Content:  content,
		Media:    []string{},
		Metadata: map[string]any{},
	}
}
