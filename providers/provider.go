package providers

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	einoark "github.com/cloudwego/eino-ext/components/model/ark"
	einoclaude "github.com/cloudwego/eino-ext/components/model/claude"
	einodeepseek "github.com/cloudwego/eino-ext/components/model/deepseek"
	einogemini "github.com/cloudwego/eino-ext/components/model/gemini"
	einoollama "github.com/cloudwego/eino-ext/components/model/ollama"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	einoqwen "github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/yangkun19921001/PP-Claw/config"
	"go.uber.org/zap"
	"google.golang.org/genai"
)

// NewChatModel 创建 Eino ChatModel。
// 优先使用 eino-ext 已提供的原生 provider 组件；仅在框架未提供原生实现时，才回退到 OpenAI-compatible 适配层。
// modelOverride 非空时覆盖 cfg.Agents.Defaults.Model，用于多 Agent 场景。
func NewChatModel(logger *zap.Logger, cfg *config.Config, modelOverride string) (model.ToolCallingChatModel, error) {
	modelName := cfg.Agents.Defaults.Model
	if modelOverride != "" {
		modelName = modelOverride
	}

	azureCfg := cfg.Providers.AzureOpenAI
	if azureCfg.APIKey != "" && azureCfg.APIBase != "" {
		azModel := azureCfg.DefaultModel
		if azModel == "" {
			azModel = modelName
		}
		logger.Info("Using Azure OpenAI provider",
			zap.String("model", azModel),
			zap.String("api_base", azureCfg.APIBase),
		)
		return NewAzureChatModel(AzureConfig{
			APIKey:       azureCfg.APIKey,
			APIBase:      azureCfg.APIBase,
			DefaultModel: azModel,
			APIVersion:   azureCfg.APIVersion,
			MaxTokens:    cfg.Agents.Defaults.MaxTokens,
			Temperature:  cfg.Agents.Defaults.Temperature,
		}, logger), nil
	}

	providerName := cfg.GetProviderName(modelName)
	provider := cfg.GetProvider(modelName)
	if providerName == "" {
		return nil, fmt.Errorf("无法为模型 %q 匹配 provider，请检查 providers 配置或 model 前缀", modelName)
	}
	if provider == nil {
		return nil, fmt.Errorf("模型 %q 命中了 provider %q，但该 provider 当前未完成配置", modelName, providerName)
	}

	apiBase := cfg.GetAPIBase(modelName)
	actualModel := resolveActualModel(modelName, providerName, provider)
	headers := buildProviderHeaders(providerName, provider)

	logger.Info("Provider 路由已解析",
		zap.String("provider", providerName),
		zap.String("requested_model", modelName),
		zap.String("actual_model", actualModel),
		zap.String("api_base", apiBase),
	)

	var (
		chatModel model.ToolCallingChatModel
		err       error
	)

	switch providerName {
	case "anthropic":
		chatModel, err = newClaudeChatModel(actualModel, apiBase, provider, cfg, headers)
	case "gemini":
		chatModel, err = newGeminiChatModel(actualModel, apiBase, provider, cfg, headers)
	case "volcengine":
		chatModel, err = newArkChatModel(actualModel, apiBase, provider, cfg, headers)
	case "deepseek":
		chatModel, err = newDeepSeekChatModel(actualModel, apiBase, provider, cfg, headers)
	case "dashscope":
		chatModel, err = newQwenChatModel(actualModel, apiBase, provider, cfg, headers)
	case "ollama":
		chatModel, err = newOllamaChatModel(actualModel, apiBase, provider, cfg, headers)
	default:
		chatModel, err = newOpenAICompatibleChatModel(actualModel, apiBase, providerName, provider, cfg, headers)
	}
	if err != nil {
		return nil, fmt.Errorf("创建 provider=%s model=%s 失败: %w", providerName, actualModel, err)
	}

	logger.Info("Provider 初始化完成",
		zap.String("provider", providerName),
		zap.String("model", actualModel),
		zap.String("api_base", apiBase),
	)

	return chatModel, nil
}

func newOpenAICompatibleChatModel(actualModel, apiBase, providerName string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string) (model.ToolCallingChatModel, error) {
	chatModelCfg := &einoopenai.ChatModelConfig{
		APIKey:  provider.APIKey,
		Model:   actualModel,
		BaseURL: apiBase,
	}
	applyCommonParamsToOpenAI(chatModelCfg, cfg)
	chatModelCfg.HTTPClient = buildHTTPClient(headers)
	return einoopenai.NewChatModel(context.Background(), chatModelCfg)
}

func newClaudeChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string) (model.ToolCallingChatModel, error) {
	chatCfg := &einoclaude.Config{
		APIKey:     provider.APIKey,
		Model:      actualModel,
		MaxTokens:  max(cfg.Agents.Defaults.MaxTokens, 1),
		HTTPClient: buildHTTPClient(headers),
	}
	if apiBase != "" {
		chatCfg.BaseURL = &apiBase
	}
	if cfg.Agents.Defaults.Temperature > 0 {
		temp := float32(cfg.Agents.Defaults.Temperature)
		chatCfg.Temperature = &temp
	}
	return einoclaude.NewChatModel(context.Background(), chatCfg)
}

func newGeminiChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string) (model.ToolCallingChatModel, error) {
	clientCfg := &genai.ClientConfig{
		APIKey:  provider.APIKey,
		Backend: genai.BackendGeminiAPI,
	}
	if apiBase != "" || len(headers) > 0 {
		httpHeaders := http.Header{}
		for k, v := range headers {
			httpHeaders.Set(k, v)
		}
		clientCfg.HTTPOptions = genai.HTTPOptions{
			BaseURL: apiBase,
			Headers: httpHeaders,
		}
	}
	client, err := genai.NewClient(context.Background(), clientCfg)
	if err != nil {
		return nil, err
	}

	chatCfg := &einogemini.Config{
		Client: client,
		Model:  actualModel,
	}
	if cfg.Agents.Defaults.MaxTokens > 0 {
		maxTokens := cfg.Agents.Defaults.MaxTokens
		chatCfg.MaxTokens = &maxTokens
	}
	if cfg.Agents.Defaults.Temperature > 0 {
		temp := float32(cfg.Agents.Defaults.Temperature)
		chatCfg.Temperature = &temp
	}
	return einogemini.NewChatModel(context.Background(), chatCfg)
}

func newArkChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string) (model.ToolCallingChatModel, error) {
	chatCfg := &einoark.ChatModelConfig{
		APIKey:       provider.APIKey,
		BaseURL:      apiBase,
		Model:        actualModel,
		HTTPClient:   buildHTTPClient(headers),
		CustomHeader: cloneStringMap(headers),
	}
	if cfg.Agents.Defaults.MaxTokens > 0 {
		maxTokens := cfg.Agents.Defaults.MaxTokens
		chatCfg.MaxTokens = &maxTokens
	}
	if cfg.Agents.Defaults.Temperature > 0 {
		temp := float32(cfg.Agents.Defaults.Temperature)
		chatCfg.Temperature = &temp
	}
	return einoark.NewChatModel(context.Background(), chatCfg)
}

func newDeepSeekChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string) (model.ToolCallingChatModel, error) {
	chatCfg := &einodeepseek.ChatModelConfig{
		APIKey:     provider.APIKey,
		BaseURL:    apiBase,
		Model:      actualModel,
		HTTPClient: buildHTTPClient(headers),
	}
	if cfg.Agents.Defaults.MaxTokens > 0 {
		chatCfg.MaxTokens = cfg.Agents.Defaults.MaxTokens
	}
	if cfg.Agents.Defaults.Temperature > 0 {
		chatCfg.Temperature = float32(cfg.Agents.Defaults.Temperature)
	}
	return einodeepseek.NewChatModel(context.Background(), chatCfg)
}

func newQwenChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string) (model.ToolCallingChatModel, error) {
	chatCfg := &einoqwen.ChatModelConfig{
		APIKey:     provider.APIKey,
		BaseURL:    apiBase,
		Model:      actualModel,
		HTTPClient: buildHTTPClient(headers),
	}
	if cfg.Agents.Defaults.MaxTokens > 0 {
		maxTokens := cfg.Agents.Defaults.MaxTokens
		chatCfg.MaxTokens = &maxTokens
	}
	if cfg.Agents.Defaults.Temperature > 0 {
		temp := float32(cfg.Agents.Defaults.Temperature)
		chatCfg.Temperature = &temp
	}
	return einoqwen.NewChatModel(context.Background(), chatCfg)
}

func newOllamaChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string) (model.ToolCallingChatModel, error) {
	chatCfg := &einoollama.ChatModelConfig{
		BaseURL:    apiBase,
		Model:      actualModel,
		HTTPClient: buildHTTPClient(headers),
		Timeout:    5 * time.Minute,
	}
	// Ollama 通过 Options 传递 Temperature 和 NumPredict(MaxTokens)
	opts := &einoollama.Options{}
	if cfg.Agents.Defaults.Temperature > 0 {
		opts.Temperature = float32(cfg.Agents.Defaults.Temperature)
	}
	if cfg.Agents.Defaults.MaxTokens > 0 {
		opts.NumPredict = cfg.Agents.Defaults.MaxTokens
	}
	chatCfg.Options = opts
	return einoollama.NewChatModel(context.Background(), chatCfg)
}

func applyCommonParamsToOpenAI(chatModelCfg *einoopenai.ChatModelConfig, cfg *config.Config) {
	if cfg.Agents.Defaults.MaxTokens > 0 {
		maxTokens := cfg.Agents.Defaults.MaxTokens
		chatModelCfg.MaxTokens = &maxTokens
	}
	if cfg.Agents.Defaults.Temperature > 0 {
		temp := float32(cfg.Agents.Defaults.Temperature)
		chatModelCfg.Temperature = &temp
	}
}

func resolveActualModel(modelName, providerName string, provider *config.ProviderConfig) string {
	if provider != nil && provider.Model != "" {
		return provider.Model
	}

	candidates := []string{providerName + "/"}
	if spec := FindByName(providerName); spec != nil {
		candidates = append(candidates, spec.SkipPrefixes...)
	}

	modelLower := strings.ToLower(modelName)
	for _, prefix := range candidates {
		if prefix == "" {
			continue
		}
		if strings.HasPrefix(modelLower, strings.ToLower(prefix)) {
			return modelName[len(prefix):]
		}
	}
	return modelName
}

func buildProviderHeaders(providerName string, provider *config.ProviderConfig) map[string]string {
	headers := cloneStringMap(provider.ExtraHeaders)

	spec := FindByName(providerName)
	if spec != nil && spec.SupportsPromptCache && providerName == "anthropic" {
		headers["anthropic-beta"] = "prompt-caching-2024-07-31"
	}
	return headers
}

func buildHTTPClient(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return nil
	}
	return &http.Client{
		Transport: &headerRoundTripper{
			base:    http.DefaultTransport,
			headers: headers,
		},
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	return maps.Clone(in)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// headerRoundTripper 自定义 RoundTripper，用于注入 HTTP 请求头。
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone request to avoid mutating the original (RoundTrip contract)
	cloned := req.Clone(req.Context())
	for k, v := range t.headers {
		cloned.Header.Set(k, v)
	}
	return t.base.RoundTrip(cloned)
}
