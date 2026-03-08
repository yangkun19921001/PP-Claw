package tools

import "go.uber.org/zap"

// FeishuToolsConfig 飞书工具创建配置
type FeishuToolsConfig struct {
	AppID            string
	AppSecret        string
	OAuthRedirectURL string
	SearchMaxResults int
	Logger           *zap.Logger
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
		result = append(result, f(cfg))
	}
	return result
}
