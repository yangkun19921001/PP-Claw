package providers

// ProviderSpec Provider 元数据 (对标 nanobot/providers/registry.py:ProviderSpec)
type ProviderSpec struct {
	Name                string
	Keywords            []string
	EnvKey              string
	DisplayName         string
	LiteLLMPrefix       string
	SkipPrefixes        []string
	EnvExtras           [][2]string // [(envKey, valueTemplate)]
	IsGateway           bool
	IsLocal             bool
	DetectByKeyPrefix   string
	DetectByBaseKeyword string
	DefaultAPIBase      string
	StripModelPrefix    bool
	IsOAuth             bool
	IsDirect            bool
	SupportsPromptCache bool
}

// Label 显示名称
func (p *ProviderSpec) Label() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Name
}

// Providers 全局注册表 (对标 registry.py:PROVIDERS，顺序=优先级)
var Providers = []ProviderSpec{
	// === Custom (direct OpenAI-compatible endpoint) ===
	{
		Name:        "custom",
		Keywords:    nil,
		EnvKey:      "",
		DisplayName: "Custom",
		IsDirect:    true,
	},

	// === Gateways ===
	{
		Name:                "openrouter",
		Keywords:            []string{"openrouter"},
		EnvKey:              "OPENROUTER_API_KEY",
		DisplayName:         "OpenRouter",
		LiteLLMPrefix:       "openrouter",
		IsGateway:           true,
		DetectByKeyPrefix:   "sk-or-",
		DetectByBaseKeyword: "openrouter",
		DefaultAPIBase:      "https://openrouter.ai/api/v1",
		SupportsPromptCache: true,
	},
	{
		Name:                "aihubmix",
		Keywords:            []string{"aihubmix"},
		EnvKey:              "OPENAI_API_KEY",
		DisplayName:         "AiHubMix",
		LiteLLMPrefix:       "openai",
		IsGateway:           true,
		DetectByBaseKeyword: "aihubmix",
		DefaultAPIBase:      "https://aihubmix.com/v1",
		StripModelPrefix:    true,
	},
	{
		Name:                "siliconflow",
		Keywords:            []string{"siliconflow"},
		EnvKey:              "OPENAI_API_KEY",
		DisplayName:         "SiliconFlow",
		LiteLLMPrefix:       "openai",
		IsGateway:           true,
		DetectByBaseKeyword: "siliconflow",
		DefaultAPIBase:      "https://api.siliconflow.cn/v1",
	},
	{
		Name:                "volcengine",
		Keywords:            []string{"volcengine", "volces", "ark"},
		EnvKey:              "OPENAI_API_KEY",
		DisplayName:         "VolcEngine",
		LiteLLMPrefix:       "volcengine",
		IsGateway:           true,
		DetectByBaseKeyword: "volces",
		DefaultAPIBase:      "https://ark.cn-beijing.volces.com/api/v3",
	},

	// === Standard providers ===
	{
		Name:                "anthropic",
		Keywords:            []string{"anthropic", "claude"},
		EnvKey:              "ANTHROPIC_API_KEY",
		DisplayName:         "Anthropic",
		SupportsPromptCache: true,
	},
	{
		Name:        "openai",
		Keywords:    []string{"openai", "gpt"},
		EnvKey:      "OPENAI_API_KEY",
		DisplayName: "OpenAI",
	},
	{
		Name:                "openai_codex",
		Keywords:            []string{"openai-codex", "codex"},
		DisplayName:         "OpenAI Codex",
		DetectByBaseKeyword: "codex",
		DefaultAPIBase:      "https://chatgpt.com/backend-api",
		IsOAuth:             true,
	},
	{
		Name:          "github_copilot",
		Keywords:      []string{"github_copilot", "copilot"},
		DisplayName:   "Github Copilot",
		LiteLLMPrefix: "github_copilot",
		SkipPrefixes:  []string{"github_copilot/"},
		IsOAuth:       true,
	},
	{
		Name:          "deepseek",
		Keywords:      []string{"deepseek"},
		EnvKey:        "DEEPSEEK_API_KEY",
		DisplayName:   "DeepSeek",
		LiteLLMPrefix: "deepseek",
		SkipPrefixes:  []string{"deepseek/"},
	},
	{
		Name:          "gemini",
		Keywords:      []string{"gemini"},
		EnvKey:        "GEMINI_API_KEY",
		DisplayName:   "Gemini",
		LiteLLMPrefix: "gemini",
		SkipPrefixes:  []string{"gemini/"},
	},
	{
		Name:          "zhipu",
		Keywords:      []string{"zhipu", "glm", "zai"},
		EnvKey:        "ZAI_API_KEY",
		DisplayName:   "Zhipu AI",
		LiteLLMPrefix: "zai",
		SkipPrefixes:  []string{"zhipu/", "zai/", "openrouter/", "hosted_vllm/"},
		EnvExtras:     [][2]string{{"ZHIPUAI_API_KEY", "{api_key}"}},
	},
	{
		Name:          "dashscope",
		Keywords:      []string{"qwen", "dashscope"},
		EnvKey:        "DASHSCOPE_API_KEY",
		DisplayName:   "DashScope",
		LiteLLMPrefix: "dashscope",
		SkipPrefixes:  []string{"dashscope/", "openrouter/"},
	},
	{
		Name:           "moonshot",
		Keywords:       []string{"moonshot", "kimi"},
		EnvKey:         "MOONSHOT_API_KEY",
		DisplayName:    "Moonshot",
		LiteLLMPrefix:  "moonshot",
		SkipPrefixes:   []string{"moonshot/", "openrouter/"},
		EnvExtras:      [][2]string{{"MOONSHOT_API_BASE", "{api_base}"}},
		DefaultAPIBase: "https://api.moonshot.ai/v1",
	},
	{
		Name:           "minimax",
		Keywords:       []string{"minimax"},
		EnvKey:         "MINIMAX_API_KEY",
		DisplayName:    "MiniMax",
		LiteLLMPrefix:  "minimax",
		SkipPrefixes:   []string{"minimax/", "openrouter/"},
		DefaultAPIBase: "https://api.minimax.io/v1",
	},

	// === Local ===
	{
		Name:          "vllm",
		Keywords:      []string{"vllm"},
		EnvKey:        "HOSTED_VLLM_API_KEY",
		DisplayName:   "vLLM/Local",
		LiteLLMPrefix: "hosted_vllm",
		IsLocal:       true,
	},

	// === Auxiliary ===
	{
		Name:          "groq",
		Keywords:      []string{"groq"},
		EnvKey:        "GROQ_API_KEY",
		DisplayName:   "Groq",
		LiteLLMPrefix: "groq",
		SkipPrefixes:  []string{"groq/"},
	},
}

// FindByModel 根据模型名匹配标准 Provider (对标 registry.py:find_by_model)
func FindByModel(model string) *ProviderSpec {
	modelLower := toLower(model)
	modelNorm := replaceAll(modelLower, "-", "_")
	modelPrefix := ""
	if idx := indexOf(modelLower, "/"); idx >= 0 {
		modelPrefix = modelLower[:idx]
	}
	normPrefix := replaceAll(modelPrefix, "-", "_")

	// 排除 gateway/local
	var stdSpecs []*ProviderSpec
	for i := range Providers {
		if !Providers[i].IsGateway && !Providers[i].IsLocal {
			stdSpecs = append(stdSpecs, &Providers[i])
		}
	}

	// 优先匹配显式 prefix
	if modelPrefix != "" {
		for _, spec := range stdSpecs {
			if normPrefix == spec.Name {
				return spec
			}
		}
	}

	// 关键词匹配
	for _, spec := range stdSpecs {
		for _, kw := range spec.Keywords {
			kwNorm := replaceAll(kw, "-", "_")
			if contains(modelLower, kw) || contains(modelNorm, kwNorm) {
				return spec
			}
		}
	}
	return nil
}

// FindGateway 检测 gateway/local Provider (对标 registry.py:find_gateway)
func FindGateway(providerName, apiKey, apiBase string) *ProviderSpec {
	// 1. 按 name 直接匹配
	if providerName != "" {
		spec := FindByName(providerName)
		if spec != nil && (spec.IsGateway || spec.IsLocal) {
			return spec
		}
	}

	// 2. 按 api_key prefix / api_base keyword 检测
	for i := range Providers {
		spec := &Providers[i]
		if spec.DetectByKeyPrefix != "" && apiKey != "" && hasPrefix(apiKey, spec.DetectByKeyPrefix) {
			return spec
		}
		if spec.DetectByBaseKeyword != "" && apiBase != "" && contains(apiBase, spec.DetectByBaseKeyword) {
			return spec
		}
	}
	return nil
}

// FindByName 按 name 查找 (对标 registry.py:find_by_name)
func FindByName(name string) *ProviderSpec {
	for i := range Providers {
		if Providers[i].Name == name {
			return &Providers[i]
		}
	}
	return nil
}

// string helpers (避免导入 strings 包冲突)
func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func replaceAll(s, old, new string) string {
	result := ""
	for i := 0; i < len(s); {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			result += new
			i += len(old)
		} else {
			result += string(s[i])
			i++
		}
	}
	return result
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func indexOf(s string, sep string) int {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
