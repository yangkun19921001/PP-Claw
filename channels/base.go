package channels

import (
	"context"
	"fmt"

	"github.com/yangkun19921001/PP-Claw/bus"
	"go.uber.org/zap"
)

// Channel 渠道接口 (对标 pp-claw/channels/base.py:BaseChannel)
type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
	Send(msg *bus.OutboundMessage) error
}

// BaseChannel 渠道基类
type BaseChannel struct {
	ChannelName string
	Bus         *bus.MessageBus
	AllowFrom   []string
	Logger      *zap.Logger
	Running     bool
}

// IsAllowed 检查发送者是否被允许 (对标 base.py:is_allowed)
func (c *BaseChannel) IsAllowed(senderID string) bool {
	if len(c.AllowFrom) == 0 {
		return true // 空列表允许所有人
	}
	for _, allowed := range c.AllowFrom {
		if allowed == senderID {
			return true
		}
	}
	return false
}

// HandleMessage 处理入站消息 (对标 base.py:_handle_message)
func (c *BaseChannel) HandleMessage(senderID, chatID, content string, media []string, metadata map[string]any) {
	if !c.IsAllowed(senderID) {
		c.Logger.Warn("访问被拒绝",
			zap.String("sender", senderID),
			zap.String("channel", c.ChannelName),
		)
		return
	}

	msg := &bus.InboundMessage{
		Channel:  c.ChannelName,
		SenderID: senderID,
		ChatID:   chatID,
		Content:  content,
		Media:    media,
		Metadata: metadata,
	}
	if msg.Media == nil {
		msg.Media = []string{}
	}
	if msg.Metadata == nil {
		msg.Metadata = map[string]any{}
	}

	c.Bus.PublishInbound(msg)
}

// ChannelFactory 渠道工厂函数类型
type ChannelFactory func(bus *bus.MessageBus, logger *zap.Logger) (Channel, error)

// channelFactories 全局渠道工厂注册表
var channelFactories = map[string]ChannelFactory{}

// RegisterFactory 注册渠道工厂
func RegisterFactory(name string, factory ChannelFactory) {
	channelFactories[name] = factory
}

// GetFactory 获取渠道工厂
func GetFactory(name string) (ChannelFactory, error) {
	f, ok := channelFactories[name]
	if !ok {
		return nil, fmt.Errorf("unknown channel: %s", name)
	}
	return f, nil
}
