package channels

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/config"
	"go.uber.org/zap"
)

// Manager 渠道管理器 — 每个渠道的每个账号一个独立 Channel 实例
// instances key 格式: "channel:accountID"
type Manager struct {
	config    *config.Config
	bus       *bus.MessageBus
	instances map[string]Channel  // key: "channel:accountID"
	defaults  map[string]string   // channelName -> defaultAccountID
	logger    *zap.Logger
	mu        sync.RWMutex
}

// NewManager 创建渠道管理器
func NewManager(cfg *config.Config, msgBus *bus.MessageBus, logger *zap.Logger) *Manager {
	m := &Manager{
		config:    cfg,
		bus:       msgBus,
		instances: make(map[string]Channel),
		defaults:  make(map[string]string),
		logger:    logger,
	}
	m.initChannels()
	return m
}

// initChannels 根据配置动态初始化渠道，支持多账号
func (m *Manager) initChannels() {
	enabledMap := m.config.Channels.GetEnabledMap()

	for _, name := range ListFactories() {
		if !enabledMap[name] {
			continue
		}

		// wechat_personal 特殊处理：单实例，内部管理多账号
		if name == "wechat_personal" {
			m.initSingleInstance(name, "default")
			m.defaults[name] = "default"
			continue
		}

		accountIDs := m.resolveChannelAccountIDs(name)
		defaultID := m.resolveDefaultAccountID(name)
		m.defaults[name] = defaultID

		for _, accountID := range accountIDs {
			m.initSingleInstance(name, accountID)
		}
	}
}

// initSingleInstance 创建并配置一个渠道实例
func (m *Manager) initSingleInstance(name, accountID string) {
	factory, err := GetFactory(name)
	if err != nil {
		m.logger.Debug("渠道工厂未注册", zap.String("channel", name))
		return
	}
	ch, err := factory(m.bus, m.logger)
	if err != nil {
		m.logger.Error("初始化渠道失败",
			zap.String("channel", name),
			zap.String("account_id", accountID),
			zap.Error(err),
		)
		return
	}

	ch.SetAccountID(accountID)
	m.configureChannelAccount(name, accountID, ch)

	key := name + ":" + accountID
	m.instances[key] = ch
	m.logger.Info("渠道实例已创建",
		zap.String("channel", name),
		zap.String("account_id", accountID),
	)
}

// resolveChannelAccountIDs 返回渠道的所有账号 ID 列表
func (m *Manager) resolveChannelAccountIDs(name string) []string {
	switch name {
	case "telegram":
		return mapKeys(m.config.Channels.Telegram.ResolveAccounts())
	case "discord":
		return mapKeys(m.config.Channels.Discord.ResolveAccounts())
	case "slack":
		return mapKeys(m.config.Channels.Slack.ResolveAccounts())
	case "whatsapp":
		return mapKeys(m.config.Channels.WhatsApp.ResolveAccounts())
	case "feishu":
		return mapKeys(m.config.Channels.Feishu.ResolveAccounts())
	case "dingtalk":
		return mapKeys(m.config.Channels.DingTalk.ResolveAccounts())
	case "email":
		return mapKeys(m.config.Channels.Email.ResolveAccounts())
	case "qq":
		return mapKeys(m.config.Channels.QQ.ResolveAccounts())
	case "mochat":
		return mapKeys(m.config.Channels.Mochat.ResolveAccounts())
	case "wecom":
		return mapKeys(m.config.Channels.Wecom.ResolveAccounts())
	case "matrix":
		return mapKeys(m.config.Channels.Matrix.ResolveAccounts())
	default:
		return []string{"default"}
	}
}

// resolveDefaultAccountID 返回渠道的默认账号 ID
func (m *Manager) resolveDefaultAccountID(name string) string {
	switch name {
	case "telegram":
		return m.config.Channels.Telegram.DefaultAccountID()
	case "discord":
		return m.config.Channels.Discord.DefaultAccountID()
	case "slack":
		return m.config.Channels.Slack.DefaultAccountID()
	case "whatsapp":
		return m.config.Channels.WhatsApp.DefaultAccountID()
	case "feishu":
		return m.config.Channels.Feishu.DefaultAccountID()
	case "dingtalk":
		return m.config.Channels.DingTalk.DefaultAccountID()
	case "email":
		return m.config.Channels.Email.DefaultAccountID()
	case "qq":
		return m.config.Channels.QQ.DefaultAccountID()
	case "mochat":
		return m.config.Channels.Mochat.DefaultAccountID()
	case "wecom":
		return m.config.Channels.Wecom.DefaultAccountID()
	case "matrix":
		return m.config.Channels.Matrix.DefaultAccountID()
	default:
		return "default"
	}
}

// configureChannelAccount 根据合并后的账号配置对渠道进行参数注入
func (m *Manager) configureChannelAccount(name, accountID string, ch Channel) {
	switch name {
	case "feishu":
		type feishuConfigurable interface {
			Configure(appID, appSecret, encryptKey, verificationToken string, allowFrom []string)
			ConfigureExtended(groupPolicy, reactEmoji string, replyToMessage bool)
		}
		if fc, ok := ch.(feishuConfigurable); ok {
			accounts := m.config.Channels.Feishu.ResolveAccounts()
			cfg := accounts[accountID]
			fc.Configure(cfg.AppID, cfg.AppSecret, cfg.EncryptKey, cfg.VerificationToken, cfg.AllowFrom)
			fc.ConfigureExtended(cfg.GroupPolicy, cfg.ReactEmoji, cfg.ReplyToMessage)
		}
	case "telegram":
		if tc, ok := ch.(*TelegramChannel); ok {
			accounts := m.config.Channels.Telegram.ResolveAccounts()
			cfg := accounts[accountID]
			tc.Configure(cfg.Token, cfg.AllowFrom, cfg.Proxy)
		}
	case "wecom":
		if wc, ok := ch.(*WecomChannel); ok {
			accounts := m.config.Channels.Wecom.ResolveAccounts()
			cfg := accounts[accountID]
			wc.Configure(cfg.BotID, cfg.Secret, cfg.WelcomeMessage, cfg.AllowFrom)
		}
	case "matrix":
		type matrixConfigurable interface {
			Configure(homeserver, accessToken, userID, deviceID string, e2ee bool, maxMediaBytes int, allowFrom []string, groupPolicy string, groupAllowFrom []string)
		}
		if mc, ok := ch.(matrixConfigurable); ok {
			accounts := m.config.Channels.Matrix.ResolveAccounts()
			cfg := accounts[accountID]
			mc.Configure(cfg.Homeserver, cfg.AccessToken, cfg.UserID, cfg.DeviceID, cfg.E2EEEnabled, cfg.MaxMediaBytes, cfg.AllowFrom, cfg.GroupPolicy, cfg.GroupAllowFrom)
		}
	case "wechat_personal":
		type wechatPersonalConfigurable interface {
			Configure(cfg config.WechatPersonalConfig, workspace string)
		}
		if wc, ok := ch.(wechatPersonalConfigurable); ok {
			workspace := config.ExpandHome(m.config.Agents.Defaults.Workspace)
			wc.Configure(m.config.Channels.WechatPersonal, workspace)
		}
	}
}

// StartAll 启动所有渠道实例
func (m *Manager) StartAll(ctx context.Context) error {
	if len(m.instances) == 0 {
		m.logger.Info("没有启用任何渠道")
		return nil
	}

	// 启动出站消息分发
	go m.dispatchOutbound(ctx)

	// 启动所有渠道实例
	var wg sync.WaitGroup
	for key, ch := range m.instances {
		wg.Add(1)
		go func(k string, c Channel) {
			defer wg.Done()
			m.logger.Info("启动渠道实例...", zap.String("instance", k))
			if err := c.Start(ctx); err != nil {
				m.logger.Error("渠道实例启动失败", zap.String("instance", k), zap.Error(err))
			}
		}(key, ch)
	}
	wg.Wait()
	return nil
}

// StopAll 停止所有渠道实例
func (m *Manager) StopAll() {
	m.logger.Info("停止所有渠道...")
	for key, ch := range m.instances {
		if err := ch.Stop(); err != nil {
			m.logger.Error("停止渠道失败", zap.String("instance", key), zap.Error(err))
		} else {
			m.logger.Info("渠道已停止", zap.String("instance", key))
		}
	}
}

// resolveOutboundKey 根据 channel + accountID 解析出站实例 key
// wechat_personal 内部自行管理多账号路由，始终使用单实例 key
func (m *Manager) resolveOutboundKey(channel, accountID string) string {
	if channel == "wechat_personal" {
		return "wechat_personal:default"
	}
	if accountID == "" {
		accountID = m.defaults[channel]
	}
	if accountID == "" {
		accountID = "default"
	}
	return channel + ":" + accountID
}

// dispatchOutbound 分发出站消息到各渠道实例
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

			// 按 channel + accountID 路由到目标实例
			key := m.resolveOutboundKey(msg.Channel, msg.AccountID)

			m.mu.RLock()
			ch, ok := m.instances[key]
			m.mu.RUnlock()

			if ok {
				if err := ch.Send(msg); err != nil {
					m.logger.Error("发送消息失败",
						zap.String("instance", key),
						zap.String("chat_id", msg.ChatID),
						zap.Error(err),
					)
				}
			} else {
				m.logger.Warn("目标渠道实例未注册，消息丢弃",
					zap.String("instance", key),
					zap.String("channel", msg.Channel),
					zap.String("account_id", msg.AccountID),
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
	for key := range m.instances {
		parts := strings.SplitN(key, ":", 2)
		status[parts[0]] = true
	}
	return status
}

// RegisterRoutes 将支持的 channel HTTP 路由挂载到 gateway mux
func (m *Manager) RegisterRoutes(mux *http.ServeMux) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for key, ch := range m.instances {
		registrar, ok := ch.(RouteRegistrar)
		if !ok {
			continue
		}
		registrar.RegisterRoutes(mux)
		m.logger.Info("渠道路由已注册", zap.String("instance", key))
	}
}

// StatusSnapshot 返回带运行信息的渠道状态快照（两级结构：channel → accounts）。
// 包含所有已注册工厂的渠道，未实例化的标记为 disabled。
func (m *Manager) StatusSnapshot() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channelAccounts := make(map[string]map[string]any)

	for key, ch := range m.instances {
		parts := strings.SplitN(key, ":", 2)
		chName, accID := parts[0], parts[1]

		entry := map[string]any{"enabled": true, "account_id": accID}
		if provider, ok := ch.(StatusProvider); ok {
			for k, v := range provider.Status() {
				entry[k] = v
			}
		}

		if channelAccounts[chName] == nil {
			channelAccounts[chName] = make(map[string]any)
		}
		channelAccounts[chName][accID] = entry
	}

	snapshot := make(map[string]any, len(channelAccounts))
	for chName, accounts := range channelAccounts {
		snapshot[chName] = map[string]any{
			"enabled":  true,
			"accounts": accounts,
		}
	}

	// 补全未启用/未实例化的渠道
	enabledMap := m.config.Channels.GetEnabledMap()
	for name := range enabledMap {
		if _, exists := snapshot[name]; !exists {
			snapshot[name] = map[string]any{
				"enabled":  enabledMap[name],
				"accounts": map[string]any{},
			}
		}
	}

	return snapshot
}

// EnabledChannels 获取已启用渠道列表（去重，返回渠道类型名）
func (m *Manager) EnabledChannels() []string {
	seen := make(map[string]bool)
	for key := range m.instances {
		parts := strings.SplitN(key, ":", 2)
		seen[parts[0]] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// OnlineSummary 返回在线渠道摘要字符串，如 "feishu(3) · wechat(1)"
func (m *Manager) OnlineSummary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := make(map[string]int)
	for key := range m.instances {
		parts := strings.SplitN(key, ":", 2)
		counts[parts[0]]++
	}

	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s(%d)", name, counts[name]))
	}
	return strings.Join(parts, " · ")
}

// mapKeys 从 map 中提取所有 key
func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
