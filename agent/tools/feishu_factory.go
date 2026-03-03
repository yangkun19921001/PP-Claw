package tools

// FeishuToolFactory 飞书工具工厂函数类型
type FeishuToolFactory func(appID, appSecret string) Tool

var feishuToolFactories []FeishuToolFactory

// RegisterFeishuToolFactory 注册飞书工具工厂（由 64 位平台的 init() 调用）
func RegisterFeishuToolFactory(factory FeishuToolFactory) {
	feishuToolFactories = append(feishuToolFactories, factory)
}

// CreateFeishuTools 创建所有已注册的飞书工具实例
func CreateFeishuTools(appID, appSecret string) []Tool {
	var result []Tool
	for _, f := range feishuToolFactories {
		result = append(result, f(appID, appSecret))
	}
	return result
}
