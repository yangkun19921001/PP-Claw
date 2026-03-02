package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/yangkun19921001/go-nanobot/bus"
	"go.uber.org/zap"
)

// WhatsAppChannel WhatsApp 渠道 (对标 channels/whatsapp.py - 149行)
type WhatsAppChannel struct {
	BaseChannel
	BridgeURL   string
	BridgeToken string
	connected   bool
}

func init() {
	RegisterFactory("whatsapp", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &WhatsAppChannel{
			BaseChannel: BaseChannel{
				ChannelName: "whatsapp",
				Bus:         msgBus,
				Logger:      logger,
			},
			BridgeURL: "ws://localhost:3001",
		}, nil
	})
}

func (w *WhatsAppChannel) Name() string { return "whatsapp" }

// Configure 配置 WhatsApp 渠道
func (w *WhatsAppChannel) Configure(bridgeURL, bridgeToken string, allowFrom []string) {
	w.BridgeURL = bridgeURL
	w.BridgeToken = bridgeToken
	w.AllowFrom = allowFrom
}

// Start 启动 WhatsApp（连接 Node.js Bridge）(对标 whatsapp.py:start)
func (w *WhatsAppChannel) Start(ctx context.Context) error {
	if w.BridgeURL == "" {
		return fmt.Errorf("whatsapp bridge_url not configured")
	}

	w.Running = true
	w.Logger.Info("WhatsApp 渠道启动", zap.String("bridge_url", w.BridgeURL))

	// 注: 完整实现应使用 gorilla/websocket 连接 bridge
	// WebSocket 连接循环
	for w.Running {
		select {
		case <-ctx.Done():
			return nil
		default:
			// 连接 bridge
			if err := w.connectBridge(ctx); err != nil {
				w.Logger.Warn("WhatsApp bridge 连接失败", zap.Error(err))
				time.Sleep(5 * time.Second)
				continue
			}
		}
	}
	return nil
}

func (w *WhatsAppChannel) Stop() error {
	w.Running = false
	w.connected = false
	return nil
}

// connectBridge 连接 WebSocket bridge
func (w *WhatsAppChannel) connectBridge(ctx context.Context) error {
	// 简化实现: 等待 context 取消
	// 完整实现应使用 gorilla/websocket
	<-ctx.Done()
	return nil
}

// Send 发送消息到 WhatsApp (对标 whatsapp.py:send)
func (w *WhatsAppChannel) Send(msg *bus.OutboundMessage) error {
	if !w.connected {
		return fmt.Errorf("whatsapp bridge not connected")
	}

	payload := map[string]any{
		"type": "send",
		"to":   msg.ChatID,
		"text": msg.Content,
	}
	_ = payload
	// 完整实现: 通过 WebSocket 发送 JSON
	return nil
}

// handleBridgeMessage 处理来自 bridge 的消息 (对标 whatsapp.py:_handle_bridge_message)
func (w *WhatsAppChannel) handleBridgeMessage(raw string) {
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		w.Logger.Warn("无效 JSON", zap.String("raw", raw[:min(len(raw), 100)]))
		return
	}

	msgType, _ := data["type"].(string)

	switch msgType {
	case "message":
		pn, _ := data["pn"].(string)
		sender, _ := data["sender"].(string)
		content, _ := data["content"].(string)

		userID := pn
		if userID == "" {
			userID = sender
		}
		senderID := userID
		if idx := strings.Index(userID, "@"); idx >= 0 {
			senderID = userID[:idx]
		}

		w.HandleMessage(senderID, sender, content, nil, map[string]any{
			"message_id": data["id"],
			"timestamp":  data["timestamp"],
			"is_group":   data["isGroup"],
		})

	case "status":
		status, _ := data["status"].(string)
		w.Logger.Info("WhatsApp 状态", zap.String("status", status))
		w.connected = (status == "connected")

	case "qr":
		w.Logger.Info("请在 bridge 终端扫描 QR 码连接 WhatsApp")

	case "error":
		errMsg, _ := data["error"].(string)
		w.Logger.Error("WhatsApp bridge 错误", zap.String("error", errMsg))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
