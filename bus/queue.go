package bus

import "context"

// MessageBus 异步消息总线 (对标 nanobot/bus/queue.py:MessageBus)
// 解耦渠道与 Agent 核心的通信
type MessageBus struct {
	inbound  chan *InboundMessage
	outbound chan *OutboundMessage
}

// NewMessageBus 创建消息总线
func NewMessageBus() *MessageBus {
	return &MessageBus{
		inbound:  make(chan *InboundMessage, 100),
		outbound: make(chan *OutboundMessage, 100),
	}
}

// PublishInbound 渠道 → Agent: 发布入站消息
func (b *MessageBus) PublishInbound(msg *InboundMessage) {
	b.inbound <- msg
}

// ConsumeInbound Agent 消费入站消息 (阻塞)
func (b *MessageBus) ConsumeInbound(ctx context.Context) (*InboundMessage, error) {
	select {
	case msg := <-b.inbound:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// PublishOutbound Agent → 渠道: 发布出站消息
func (b *MessageBus) PublishOutbound(msg *OutboundMessage) {
	b.outbound <- msg
}

// ConsumeOutbound 渠道消费出站消息 (阻塞)
func (b *MessageBus) ConsumeOutbound(ctx context.Context) (*OutboundMessage, error) {
	select {
	case msg := <-b.outbound:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// InboundSize 待处理入站消息数
func (b *MessageBus) InboundSize() int {
	return len(b.inbound)
}

// OutboundSize 待处理出站消息数
func (b *MessageBus) OutboundSize() int {
	return len(b.outbound)
}
