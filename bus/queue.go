package bus

import (
	"context"
	"sync"
)

// MessageBus 异步消息总线 (对标 nanobot/bus/queue.py:MessageBus)
// 解耦渠道与 Agent 核心的通信
// Outbound 使用广播模式: 每个订阅者都能收到每条消息
type MessageBus struct {
	inbound  chan *InboundMessage
	outbound chan *OutboundMessage

	mu          sync.RWMutex
	subscribers map[int]chan *OutboundMessage
	nextID      int
}

// NewMessageBus 创建消息总线
func NewMessageBus() *MessageBus {
	b := &MessageBus{
		inbound:     make(chan *InboundMessage, 100),
		outbound:    make(chan *OutboundMessage, 100),
		subscribers: make(map[int]chan *OutboundMessage),
	}
	go b.fanOut()
	return b
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

// SubscribeOutbound 订阅出站消息，返回专属 channel 和取消函数
func (b *MessageBus) SubscribeOutbound() (<-chan *OutboundMessage, func()) {
	ch := make(chan *OutboundMessage, 100)
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subscribers[id] = ch
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		delete(b.subscribers, id)
		b.mu.Unlock()
	}
	return ch, unsubscribe
}

// ConsumeOutbound 订阅并阻塞消费出站消息（便捷方法，等同于 SubscribeOutbound + select）
func (b *MessageBus) ConsumeOutbound(ctx context.Context) (*OutboundMessage, error) {
	select {
	case msg := <-b.outbound:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// fanOut 将 outbound channel 的消息广播给所有订阅者
func (b *MessageBus) fanOut() {
	for msg := range b.outbound {
		b.mu.RLock()
		for _, ch := range b.subscribers {
			select {
			case ch <- msg:
			default:
				// 订阅者缓冲区满，跳过避免阻塞
			}
		}
		b.mu.RUnlock()
	}
}

// InboundSize 待处理入站消息数
func (b *MessageBus) InboundSize() int {
	return len(b.inbound)
}
