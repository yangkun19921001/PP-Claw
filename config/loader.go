package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load 从 YAML 文件加载配置 (对标 pp-claw/config/loader.py)
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // 文件不存在，使用默认配置
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return cfg, nil
}

// Save 保存配置到 YAML 文件
func Save(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// ExpandHome 展开 ~ 为 home 目录
func ExpandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// matchProvider 根据 model 前缀匹配 Provider (对标 pp-claw/config/schema.py:_match_provider)
func (c *Config) matchProvider(model string) (*ProviderConfig, string) {
	if model == "" {
		model = c.Agents.Defaults.Model
	}
	modelLower := strings.ToLower(model)
	prefix := ""
	if idx := strings.Index(modelLower, "/"); idx >= 0 {
		prefix = modelLower[:idx]
	}

	providers := c.providerMap()

	// 按前缀精确匹配。显式指定 provider 时，不要再悄悄回退到其它 provider。
	if prefix != "" {
		if p := providers[prefix]; p != nil {
			if isProviderConfigured(prefix, p) {
				return p, prefix
			}
			return nil, prefix
		}
	}

	// 按 providers.<name>.model 精确匹配。
	for _, name := range providerOrder() {
		p := providers[name]
		if p == nil || p.Model == "" {
			continue
		}
		if strings.EqualFold(p.Model, model) {
			if isProviderConfigured(name, p) {
				return p, name
			}
			return nil, name
		}
	}

	// 只在完全无法判断 provider 时，才回退到第一个已配置 provider。
	for _, name := range providerOrder() {
		p := providers[name]
		if p != nil && isProviderConfigured(name, p) {
			return p, name
		}
	}

	return nil, ""
}

func isProviderConfigured(name string, p *ProviderConfig) bool {
	if p == nil {
		return false
	}

	switch name {
	case "ollama":
		return p.GetEffectiveAPIBase() != "" || p.Model != ""
	case "custom", "vllm":
		return p.GetEffectiveAPIBase() != "" || p.APIKey != ""
	default:
		return p.APIKey != ""
	}
}

// providerOrder fallback 匹配优先级顺序（当 model 无前缀时，按此顺序查找第一个已配置的 provider）。
// 注意：此顺序与 providers/registry.go:Providers 的关键词匹配顺序不同，两者用途不同。
func providerOrder() []string {
	return []string{
		"anthropic", "openai", "openrouter", "deepseek", "groq",
		"zhipu", "dashscope", "gemini", "ollama", "moonshot", "minimax",
		"aihubmix", "siliconflow", "volcengine", "vllm", "custom",
		"openai_codex", "github_copilot",
	}
}

// defaultAPIBase 返回 Provider 的默认 API Base
func defaultAPIBase(name string) string {
	bases := map[string]string{
		"openrouter":   "https://openrouter.ai/api/v1",
		"deepseek":     "https://api.deepseek.com/v1",
		"groq":         "https://api.groq.com/openai/v1",
		"zhipu":        "https://open.bigmodel.cn/api/paas/v4",
		"dashscope":    "https://dashscope.aliyuncs.com/compatible-mode/v1",
		"ollama":       "http://127.0.0.1:11434",
		"moonshot":     "https://api.moonshot.cn/v1",
		"minimax":      "https://api.minimax.chat/v1",
		"aihubmix":     "https://aihubmix.com/v1",
		"siliconflow":  "https://api.siliconflow.cn/v1",
		"volcengine":   "https://ark.cn-beijing.volces.com/api/v3",
		"openai_codex": "https://chatgpt.com/backend-api",
	}
	return bases[name]
}

// GetAPIBase 获取 API Base URL
func (c *Config) GetAPIBase(model string) string {
	p, name := c.matchProvider(model)
	if p != nil {
		effective := p.GetEffectiveAPIBase()
		if effective != "" {
			return effective
		}
	}
	return defaultAPIBase(name)
}
