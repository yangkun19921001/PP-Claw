package channels

import (
	"context"
	"sync"

	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/config"
	"go.uber.org/zap"
)

// Manager 渠道管理器 (对标 pp-claw/channels/manager.py:ChannelManager)
type Manager struct {
	config   *config.Config
	bus      *bus.MessageBus
	channels map[string]Channel
	logger   *zap.Logger
	mu       sync.RWMutex
}

// NewManager 创建渠道管理器
func NewManager(cfg *config.Config, msgBus *bus.MessageBus, logger *zap.Logger) *Manager {
	m := &Manager{
		config:   cfg,
		bus:      msgBus,
		channels: make(map[string]Channel),
		logger:   logger,
	}
	m.initChannels()
	return m
}

// initChannels 根据配置动态初始化渠道 — 遍历已注册工厂 + 配置的 enabled 状态
func (m *Manager) initChannels() {
	enabledMap := m.config.Channels.GetEnabledMap()

	for _, name := range ListFactories() {
		if !enabledMap[name] {
			continue
		}
		factory, err := GetFactory(name)
		if err != nil {
			m.logger.Debug("渠道工厂未注册", zap.String("channel", name))
			continue
		}
		channel, err := factory(m.bus, m.logger)
		if err != nil {
			m.logger.Error("初始化渠道失败", zap.String("channel", name), zap.Error(err))
			continue
		}
		m.configureChannel(name, channel)
		m.channels[name] = channel
		m.logger.Info("渠道已启用", zap.String("channel", name))
	}
}

// StartAll 启动所有渠道 (对标 manager.py:start_all)
func (m *Manager) StartAll(ctx context.Context) error {
	if len(m.channels) == 0 {
		m.logger.Info("没有启用任何渠道")
		return nil
	}

	// 启动出站消息分发
	go m.dispatchOutbound(ctx)

	// 启动所有渠道
	var wg sync.WaitGroup
	for name, ch := range m.channels {
		wg.Add(1)
		go func(n string, c Channel) {
			defer wg.Done()
			m.logger.Info("启动渠道...", zap.String("channel", n))
			if err := c.Start(ctx); err != nil {
				m.logger.Error("渠道启动失败", zap.String("channel", n), zap.Error(err))
			}
		}(name, ch)
	}
	wg.Wait()
	return nil
}

// StopAll 停止所有渠道 (对标 manager.py:stop_all)
func (m *Manager) StopAll() {
	m.logger.Info("停止所有渠道...")
	for name, ch := range m.channels {
		if err := ch.Stop(); err != nil {
			m.logger.Error("停止渠道失败", zap.String("channel", name), zap.Error(err))
		} else {
			m.logger.Info("渠道已停止", zap.String("channel", name))
		}
	}
}

// dispatchOutbound 分发出站消息到各渠道 (对标 manager.py:_dispatch_outbound)
func (m *Manager) dispatchOutbound(ctx context.Context) {
	m.logger.Info("出站消息分发器启动")
	sub, unsub := m.bus.SubscribeOutbound()
	defer unsub()

	for {
		select {
		case msg := <-sub:
			// 跳过 CLI 消息（由 CLI handler 处理）
			if msg.Channel == "cli" {
				continue
			}

			// 跳过进度消息（按配置）
			if isProgress, ok := msg.Metadata["_progress"].(bool); ok && isProgress {
				if isToolHint, ok := msg.Metadata["_tool_hint"].(bool); ok && isToolHint {
					if !m.config.Channels.SendToolHints {
						continue
					}
				} else if !m.config.Channels.SendProgress {
					continue
				}
			}

			// 分发到目标渠道
			m.mu.RLock()
			ch, ok := m.channels[msg.Channel]
			m.mu.RUnlock()

			if ok {
				if err := ch.Send(msg); err != nil {
					m.logger.Error("发送消息失败",
						zap.String("channel", msg.Channel),
						zap.String("chat_id", msg.ChatID),
						zap.Error(err),
					)
				}
			} else {
				m.logger.Warn("目标渠道未注册，消息丢弃",
					zap.String("channel", msg.Channel),
				)
			}
		case <-ctx.Done():
			return
		}
	}
}

// GetStatus 获取所有渠道状态
func (m *Manager) GetStatus() map[string]bool {
	status := make(map[string]bool)
	for name := range m.channels {
		status[name] = true
	}
	return status
}

// EnabledChannels 获取已启用渠道列表
func (m *Manager) EnabledChannels() []string {
	var names []string
	for name := range m.channels {
		names = append(names, name)
	}
	return names
}

// configureChannel 根据配置对渠道进行参数注入
func (m *Manager) configureChannel(name string, ch Channel) {
	switch name {
	case "feishu":
		type feishuConfigurable interface {
			Configure(appID, appSecret, encryptKey, verificationToken string, allowFrom []string)
			ConfigureExtended(groupPolicy, reactEmoji string, replyToMessage bool)
		}
		if fc, ok := ch.(feishuConfigurable); ok {
			cfg := m.config.Channels.Feishu
			fc.Configure(cfg.AppID, cfg.AppSecret, cfg.EncryptKey, cfg.VerificationToken, cfg.AllowFrom)
			fc.ConfigureExtended(cfg.GroupPolicy, cfg.ReactEmoji, cfg.ReplyToMessage)
		}
	case "telegram":
		if tc, ok := ch.(*TelegramChannel); ok {
			cfg := m.config.Channels.Telegram
			tc.Configure(cfg.Token, cfg.AllowFrom, cfg.Proxy)
		}
	case "wecom":
		if wc, ok := ch.(*WecomChannel); ok {
			cfg := m.config.Channels.Wecom
			wc.Configure(cfg.BotID, cfg.Secret, cfg.WelcomeMessage, cfg.AllowFrom)
		}
	case "matrix":
		type matrixConfigurable interface {
			Configure(homeserver, accessToken, userID, deviceID string, e2ee bool, maxMediaBytes int, allowFrom []string, groupPolicy string, groupAllowFrom []string)
		}
		if mc, ok := ch.(matrixConfigurable); ok {
			cfg := m.config.Channels.Matrix
			mc.Configure(cfg.Homeserver, cfg.AccessToken, cfg.UserID, cfg.DeviceID, cfg.E2EEEnabled, cfg.MaxMediaBytes, cfg.AllowFrom, cfg.GroupPolicy, cfg.GroupAllowFrom)
		}
	}
}
