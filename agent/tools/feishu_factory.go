package tools

import (
	lark "github.com/larksuite/oapi-sdk-go/v3"
	"go.uber.org/zap"
)

// FeishuAccountCredentials 单个飞书账号的凭据
type FeishuAccountCredentials struct {
	AppID     string
	AppSecret string
}

// FeishuToolsConfig 飞书工具创建配置（多账号）
type FeishuToolsConfig struct {
	Accounts         map[string]FeishuAccountCredentials // account_id → 凭据
	DefaultAccountID string
	OAuthRedirectURL string
	SearchMaxResults int
	Logger           *zap.Logger
	// Aily 数据知识问答配置
	AilyAppID           string
	AilyDataAssetIDs    []string
	AilyDataAssetTagIDs []string
}

// feishuMultiClient 多账号 lark.Client 管理，嵌入到各飞书工具中
type feishuMultiClient struct {
	clients          map[string]*lark.Client
	defaultAccountID string
	accountID        string // 当前上下文的 account_id，由 SetContextWithAccount 设置
}

// SetContextWithAccount 实现 AccountContextSetter 接口
func (m *feishuMultiClient) SetContextWithAccount(channel, accountID, chatID string) {
	m.accountID = accountID
}

// getClient 返回当前账号对应的 lark.Client
func (m *feishuMultiClient) getClient() *lark.Client {
	id := m.accountID
	if id == "" {
		id = m.defaultAccountID
	}
	if c, ok := m.clients[id]; ok {
		return c
	}
	// fallback 到默认账号
	return m.clients[m.defaultAccountID]
}

// buildClients 根据配置创建所有账号的 lark.Client
func buildClients(cfg *FeishuToolsConfig) map[string]*lark.Client {
	clients := make(map[string]*lark.Client, len(cfg.Accounts))
	for id, acct := range cfg.Accounts {
		clients[id] = lark.NewClient(acct.AppID, acct.AppSecret)
	}
	return clients
}

// FeishuToolFactory 飞书工具工厂函数类型
type FeishuToolFactory func(cfg *FeishuToolsConfig) Tool

var feishuToolFactories []FeishuToolFactory

// RegisterFeishuToolFactory 注册飞书工具工厂（由 64 位平台的 init() 调用）
func RegisterFeishuToolFactory(factory FeishuToolFactory) {
	feishuToolFactories = append(feishuToolFactories, factory)
}

// CreateFeishuTools 创建所有已注册的飞书工具实例
func CreateFeishuTools(cfg *FeishuToolsConfig) []Tool {
	var result []Tool
	for _, f := range feishuToolFactories {
		if t := f(cfg); t != nil {
			result = append(result, t)
		}
	}
	return result
}
