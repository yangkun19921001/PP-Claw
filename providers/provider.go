package providers

import (
	"context"
	"fmt"
	"net/http"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/yangkun19921001/PP-Claw/config"
	"go.uber.org/zap"
)

// NewChatModel 创建 Eino ChatModel (对标 pp-claw/providers/litellm_provider.py)
// 使用 OpenAI 兼容层，支持所有 OpenAI 兼容 API
//
// 支持的配置方式:
//  1. 内置 Provider: model="deepseek/deepseek-chat" + providers.deepseek.api_key
//  2. 自定义 Provider: model="custom/my-model" 或 model="my-model" + providers.custom.api_key + providers.custom.api_base
//  3. 任意 OpenAI 兼容 API: 配置 providers.custom 即可
func NewChatModel(logger *zap.Logger, cfg *config.Config) (model.ToolCallingChatModel, error) {
	modelName := cfg.Agents.Defaults.Model
	provider := cfg.GetProvider(modelName)
	providerName := cfg.GetProviderName(modelName)

	if provider == nil || provider.APIKey == "" {
		return nil, fmt.Errorf("no API key configured for model %q\n\nPlease configure in ~/.pp-claw/pp-claw.yaml:\n\n  providers:\n    custom:\n      api_key: \"your-api-key\"\n      api_base: \"https://your-api-base/v1\"\n\n  agents:\n    defaults:\n      model: \"your-model-name\"", modelName)
	}

	// 获取 API Base URL
	apiBase := cfg.GetAPIBase(modelName)
	if apiBase == "" {
		apiBase = "https://api.openai.com/v1"
	}

	// 直接使用用户填写的 model 名，不做前缀剥离
	actualModel := modelName

	// 如果 Provider 配置中指定了 model，使用 Provider 级别的 model 覆盖
	if provider.Model != "" {
		actualModel = provider.Model
	}

	// 构建 ChatModel 配置
	chatModelCfg := &einoopenai.ChatModelConfig{
		APIKey:  provider.APIKey,
		Model:   actualModel,
		BaseURL: apiBase,
	}

	// 设置可选参数
	if cfg.Agents.Defaults.MaxTokens > 0 {
		maxTokens := cfg.Agents.Defaults.MaxTokens
		chatModelCfg.MaxTokens = &maxTokens
	}
	if cfg.Agents.Defaults.Temperature > 0 {
		temp := float32(cfg.Agents.Defaults.Temperature)
		chatModelCfg.Temperature = &temp
	}

	// 应用 Prompt Caching 和 Extra Headers
	applyPromptCaching(providerName, provider, chatModelCfg)

	chatModel, err := einoopenai.NewChatModel(context.Background(), chatModelCfg)
	if err != nil {
		return nil, fmt.Errorf("创建 ChatModel 失败: %w", err)
	}

	logger.Info("Provider 初始化完成",
		zap.String("model", actualModel),
		zap.String("api_base", apiBase),
		zap.String("provider", providerName),
	)

	return chatModel, nil
}

// applyPromptCaching 应用 Prompt Caching 支持
// 通过自定义 HTTPClient 注入 extra headers (包括 anthropic-beta header)
func applyPromptCaching(providerName string, provider *config.ProviderConfig, chatModelCfg *einoopenai.ChatModelConfig) {
	headers := make(map[string]string)

	// 从 Provider 配置传递 extra_headers
	for k, v := range provider.ExtraHeaders {
		headers[k] = v
	}

	// 对 Anthropic/OpenRouter 自动注入 prompt caching beta header
	spec := FindByName(providerName)
	if spec != nil && spec.SupportsPromptCache {
		if providerName == "anthropic" {
			headers["anthropic-beta"] = "prompt-caching-2024-07-31"
		}
	}

	if len(headers) > 0 {
		chatModelCfg.HTTPClient = &http.Client{
			Transport: &headerRoundTripper{
				base:    http.DefaultTransport,
				headers: headers,
			},
		}
	}
}

// headerRoundTripper 自定义 RoundTripper，用于注入 HTTP 请求头
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}
