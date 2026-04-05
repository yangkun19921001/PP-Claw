package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

const (
	// llmResponseHeaderTimeout 等待 LLM API 返回响应头的最长时间。
	// 使用 ResponseHeaderTimeout 而非 Client.Timeout，避免中断 streaming 响应。
	llmResponseHeaderTimeout = 5 * time.Minute
	// llmRetryMax 5xx / 网络错误最大重试次数。
	llmRetryMax = 2
	// llmRetryBaseDelay 重试指数退避基础延迟（3s → 6s）。
	llmRetryBaseDelay = 3 * time.Second
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
		chatModel, err = newClaudeChatModel(actualModel, apiBase, provider, cfg, headers, logger)
	case "gemini":
		chatModel, err = newGeminiChatModel(actualModel, apiBase, provider, cfg, headers, logger)
	case "volcengine":
		chatModel, err = newArkChatModel(actualModel, apiBase, provider, cfg, headers, logger)
	case "deepseek":
		chatModel, err = newDeepSeekChatModel(actualModel, apiBase, provider, cfg, headers, logger)
	case "dashscope":
		chatModel, err = newQwenChatModel(actualModel, apiBase, provider, cfg, headers, logger)
	case "ollama":
		chatModel, err = newOllamaChatModel(actualModel, apiBase, provider, cfg, headers, logger)
	default:
		chatModel, err = newOpenAICompatibleChatModel(actualModel, apiBase, providerName, provider, cfg, headers, logger)
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

func newOpenAICompatibleChatModel(actualModel, apiBase, providerName string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string, logger *zap.Logger) (model.ToolCallingChatModel, error) {
	chatModelCfg := &einoopenai.ChatModelConfig{
		APIKey:  provider.APIKey,
		Model:   actualModel,
		BaseURL: apiBase,
	}
	applyCommonParamsToOpenAI(chatModelCfg, cfg)
	chatModelCfg.HTTPClient = buildHTTPClientWithLogger(headers, logger)
	return einoopenai.NewChatModel(context.Background(), chatModelCfg)
}

func newClaudeChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string, logger *zap.Logger) (model.ToolCallingChatModel, error) {
	chatCfg := &einoclaude.Config{
		APIKey:     provider.APIKey,
		Model:      actualModel,
		MaxTokens:  max(cfg.Agents.Defaults.MaxTokens, 1),
		HTTPClient: buildHTTPClientWithLogger(headers, logger),
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

func newGeminiChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string, _ *zap.Logger) (model.ToolCallingChatModel, error) {
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

func newArkChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string, logger *zap.Logger) (model.ToolCallingChatModel, error) {
	chatCfg := &einoark.ChatModelConfig{
		APIKey:       provider.APIKey,
		BaseURL:      apiBase,
		Model:        actualModel,
		HTTPClient:   buildHTTPClientWithLogger(headers, logger),
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

func newDeepSeekChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string, logger *zap.Logger) (model.ToolCallingChatModel, error) {
	chatCfg := &einodeepseek.ChatModelConfig{
		APIKey:     provider.APIKey,
		BaseURL:    apiBase,
		Model:      actualModel,
		HTTPClient: buildHTTPClientWithLogger(headers, logger),
	}
	if cfg.Agents.Defaults.MaxTokens > 0 {
		chatCfg.MaxTokens = cfg.Agents.Defaults.MaxTokens
	}
	if cfg.Agents.Defaults.Temperature > 0 {
		chatCfg.Temperature = float32(cfg.Agents.Defaults.Temperature)
	}
	return einodeepseek.NewChatModel(context.Background(), chatCfg)
}

func newQwenChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string, logger *zap.Logger) (model.ToolCallingChatModel, error) {
	chatCfg := &einoqwen.ChatModelConfig{
		APIKey:     provider.APIKey,
		BaseURL:    apiBase,
		Model:      actualModel,
		HTTPClient: buildHTTPClientWithLogger(headers, logger),
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

func newOllamaChatModel(actualModel, apiBase string, provider *config.ProviderConfig, cfg *config.Config, headers map[string]string, logger *zap.Logger) (model.ToolCallingChatModel, error) {
	chatCfg := &einoollama.ChatModelConfig{
		BaseURL:    apiBase,
		Model:      actualModel,
		HTTPClient: buildHTTPClientWithLogger(headers, logger),
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
	if headers == nil {
		headers = make(map[string]string)
	}

	spec := FindByName(providerName)
	if spec != nil && spec.SupportsPromptCache && providerName == "anthropic" {
		headers["anthropic-beta"] = "prompt-caching-2024-07-31"
	}
	return headers
}

func buildHTTPClient(headers map[string]string) *http.Client {
	return buildHTTPClientWithLogger(headers, nil)
}

func buildHTTPClientWithLogger(headers map[string]string, logger *zap.Logger) *http.Client {
	// Clone default transport, add response header timeout
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	transport := base.Clone()
	transport.ResponseHeaderTimeout = llmResponseHeaderTimeout

	// Wrap with retry
	var rt http.RoundTripper = &retryRoundTripper{
		base:       transport,
		maxRetries: llmRetryMax,
		baseDelay:  llmRetryBaseDelay,
	}

	// Wrap with custom headers if needed
	if len(headers) > 0 {
		rt = &headerRoundTripper{
			base:    rt,
			headers: headers,
		}
	}

	// Wrap with debug logging
	if logger != nil {
		rt = &loggingRoundTripper{base: rt, logger: logger}
	}

	return &http.Client{Transport: rt}
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

// retryRoundTripper 在 5xx 或网络错误时自动重试，指数退避。
type retryRoundTripper struct {
	base       http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
}

func (t *retryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer request body for potential retries
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		if attempt > 0 {
			delay := t.baseDelay * time.Duration(1<<(attempt-1))
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(delay):
			}
		}

		attemptReq := req.Clone(req.Context())
		if bodyBytes != nil {
			attemptReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			attemptReq.ContentLength = int64(len(bodyBytes))
		} else {
			attemptReq.Body = nil
		}

		lastResp, lastErr = t.base.RoundTrip(attemptReq)
		if lastErr != nil {
			if req.Context().Err() != nil {
				return nil, req.Context().Err()
			}
			continue // network error — retry
		}

		if lastResp.StatusCode < 500 {
			return lastResp, nil // 2xx/3xx/4xx — no retry
		}

		// 5xx — drain body and retry (unless last attempt)
		if attempt < t.maxRetries {
			io.Copy(io.Discard, lastResp.Body)
			lastResp.Body.Close()
			lastResp = nil
		}
	}

	if lastResp != nil {
		return lastResp, nil // return final 5xx response
	}
	return nil, lastErr // return final network error
}

// loggingRoundTripper 记录 LLM HTTP 请求/响应摘要，用于调试。
// 对 SSE 流式响应使用 tee reader 捕获前 N 字节，不破坏 streaming。
type loggingRoundTripper struct {
	base   http.RoundTripper
	logger *zap.Logger
}

func (t *loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	// 记录请求摘要
	t.logger.Debug("LLM HTTP 请求",
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()),
		zap.Int64("content_length", req.ContentLength),
	)

	resp, err := t.base.RoundTrip(req)
	elapsed := time.Since(start)

	if err != nil {
		t.logger.Warn("LLM HTTP 错误",
			zap.String("url", req.URL.String()),
			zap.Duration("elapsed", elapsed),
			zap.Error(err),
		)
		return nil, err
	}

	ct := resp.Header.Get("Content-Type")
	isStream := strings.Contains(ct, "text/event-stream") || strings.Contains(ct, "stream")

	t.logger.Info("LLM HTTP 响应头",
		zap.String("url", req.URL.String()),
		zap.Int("status", resp.StatusCode),
		zap.Duration("elapsed", elapsed),
		zap.String("content_type", ct),
		zap.Bool("is_stream", isStream),
	)

	if isStream {
		// 流式响应：用 loggingBody 包装，在读取过程中捕获前 N 字节
		resp.Body = &loggingBody{
			inner:  resp.Body,
			logger: t.logger,
			url:    req.URL.String(),
			start:  start,
			maxCap: 1024,
		}
	} else {
		// 非流式响应：读取并记录完整 body
		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.logger.Warn("LLM HTTP 响应体读取失败",
				zap.String("url", req.URL.String()),
				zap.Int("status", resp.StatusCode),
				zap.Error(readErr),
			)
			return nil, readErr
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		preview := string(respBody)
		if len(preview) > 500 {
			preview = preview[:500] + "...(truncated)"
		}
		t.logger.Info("LLM HTTP 响应体",
			zap.String("url", req.URL.String()),
			zap.Int("status", resp.StatusCode),
			zap.Int("body_size", len(respBody)),
			zap.String("body_preview", preview),
		)
	}

	return resp, nil
}

// loggingBody 包装 ReadCloser，在读取过程中捕获前 maxCap 字节，Close 时输出日志。
type loggingBody struct {
	inner  io.ReadCloser
	logger *zap.Logger
	url    string
	start  time.Time
	maxCap int
	buf    []byte
	total  int
}

func (b *loggingBody) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	b.total += n
	if remaining := b.maxCap - len(b.buf); remaining > 0 && n > 0 {
		if n < remaining {
			remaining = n
		}
		b.buf = append(b.buf, p[:remaining]...)
	}
	return n, err
}

func (b *loggingBody) Close() error {
	err := b.inner.Close()
	preview := string(b.buf)
	if len(preview) > 500 {
		preview = preview[:500] + "...(truncated)"
	}
	b.logger.Info("LLM HTTP 流结束",
		zap.String("url", b.url),
		zap.Int("total_bytes", b.total),
		zap.Duration("stream_duration", time.Since(b.start)),
		zap.String("stream_preview", preview),
	)
	return err
}
