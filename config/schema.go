package config

import "path/filepath"

// Config 根配置 (对标 nanobot/config/schema.py:Config)
type Config struct {
	Agents    AgentsConfig    `yaml:"agents"`
	Channels  ChannelsConfig  `yaml:"channels"`
	Providers ProvidersConfig `yaml:"providers"`
	Gateway   GatewayConfig   `yaml:"gateway"`
	Tools     ToolsConfig     `yaml:"tools"`
}

// AgentsConfig Agent 配置
type AgentsConfig struct {
	Defaults AgentDefaults `yaml:"defaults"`
}

// AgentDefaults 默认 Agent 配置
type AgentDefaults struct {
	Workspace         string  `yaml:"workspace"`
	Model             string  `yaml:"model"`
	MaxTokens         int     `yaml:"max_tokens"`
	Temperature       float64 `yaml:"temperature"`
	MaxToolIterations int     `yaml:"max_tool_iterations"`
	MemoryWindow      int     `yaml:"memory_window"`
}

// ChannelsConfig 渠道配置
type ChannelsConfig struct {
	SendProgress  bool           `yaml:"send_progress"`
	SendToolHints bool           `yaml:"send_tool_hints"`
	Telegram      TelegramConfig `yaml:"telegram"`
	Discord       DiscordConfig  `yaml:"discord"`
	Slack         SlackConfig    `yaml:"slack"`
	WhatsApp      WhatsAppConfig `yaml:"whatsapp"`
	Feishu        FeishuConfig   `yaml:"feishu"`
	DingTalk      DingTalkConfig `yaml:"dingtalk"`
	Email         EmailConfig    `yaml:"email"`
	QQ            QQConfig       `yaml:"qq"`
	Mochat        MochatConfig   `yaml:"mochat"`
}

// ProviderConfig 单个 LLM Provider 配置
type ProviderConfig struct {
	APIKey       string            `yaml:"api_key"`
	APIBase      string            `yaml:"api_base"`
	BaseURL      string            `yaml:"base_url"` // api_base 的别名，优先使用
	Model        string            `yaml:"model"`    // 可选：覆盖全局 model
	ExtraHeaders map[string]string `yaml:"extra_headers"`
}

// GetEffectiveAPIBase 获取有效的 API Base URL (base_url 优先于 api_base)
func (p *ProviderConfig) GetEffectiveAPIBase() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	return p.APIBase
}

// ProvidersConfig 所有 Provider 配置
type ProvidersConfig struct {
	Custom        ProviderConfig `yaml:"custom"`
	Anthropic     ProviderConfig `yaml:"anthropic"`
	OpenAI        ProviderConfig `yaml:"openai"`
	OpenRouter    ProviderConfig `yaml:"openrouter"`
	DeepSeek      ProviderConfig `yaml:"deepseek"`
	Groq          ProviderConfig `yaml:"groq"`
	Zhipu         ProviderConfig `yaml:"zhipu"`
	DashScope     ProviderConfig `yaml:"dashscope"`
	VLLM          ProviderConfig `yaml:"vllm"`
	Gemini        ProviderConfig `yaml:"gemini"`
	Moonshot      ProviderConfig `yaml:"moonshot"`
	MiniMax       ProviderConfig `yaml:"minimax"`
	AiHubMix      ProviderConfig `yaml:"aihubmix"`
	SiliconFlow   ProviderConfig `yaml:"siliconflow"`
	VolcEngine    ProviderConfig `yaml:"volcengine"`
	OpenAICodex   ProviderConfig `yaml:"openai_codex"`
	GithubCopilot ProviderConfig `yaml:"github_copilot"`
}

// GatewayConfig Gateway 配置
type GatewayConfig struct {
	Host      string          `yaml:"host"`
	Port      int             `yaml:"port"`
	Heartbeat HeartbeatConfig `yaml:"heartbeat"`
}

// HeartbeatConfig 心跳配置
type HeartbeatConfig struct {
	Enabled   bool `yaml:"enabled"`
	IntervalS int  `yaml:"interval_s"`
}

// WebSearchConfig 网页搜索配置
type WebSearchConfig struct {
	APIKey     string `yaml:"api_key"`
	MaxResults int    `yaml:"max_results"`
}

// ExecToolConfig Shell 执行工具配置
type ExecToolConfig struct {
	Timeout int `yaml:"timeout"`
}

// MCPServerConfig MCP 服务器配置
type MCPServerConfig struct {
	Command     string            `yaml:"command"`
	Args        []string          `yaml:"args"`
	Env         map[string]string `yaml:"env"`
	URL         string            `yaml:"url"`
	Headers     map[string]string `yaml:"headers"`
	ToolTimeout int               `yaml:"tool_timeout"`
}

// ToolsConfig 工具配置
type ToolsConfig struct {
	Web                 WebToolsConfig             `yaml:"web"`
	Exec                ExecToolConfig             `yaml:"exec"`
	RestrictToWorkspace bool                       `yaml:"restrict_to_workspace"`
	MCPServers          map[string]MCPServerConfig `yaml:"mcp_servers"`
}

// WebToolsConfig Web 工具配置
type WebToolsConfig struct {
	Search WebSearchConfig `yaml:"search"`
}

// ============ 渠道子配置 (与 Python schema.py 完全对齐) ============

type TelegramConfig struct {
	Enabled        bool     `yaml:"enabled"`
	Token          string   `yaml:"token"`
	AllowFrom      []string `yaml:"allow_from"`
	Proxy          string   `yaml:"proxy"`
	ReplyToMessage bool     `yaml:"reply_to_message"`
}

type DiscordConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Token      string   `yaml:"token"`
	AllowFrom  []string `yaml:"allow_from"`
	GatewayURL string   `yaml:"gateway_url"`
	Intents    int      `yaml:"intents"`
}

// SlackDMConfig Slack DM 策略
type SlackDMConfig struct {
	Enabled   bool     `yaml:"enabled"`
	Policy    string   `yaml:"policy"` // "open" | "allowlist"
	AllowFrom []string `yaml:"allow_from"`
}

type SlackConfig struct {
	Enabled        bool          `yaml:"enabled"`
	Mode           string        `yaml:"mode"` // "socket"
	WebhookPath    string        `yaml:"webhook_path"`
	BotToken       string        `yaml:"bot_token"`
	AppToken       string        `yaml:"app_token"`
	ReplyInThread  bool          `yaml:"reply_in_thread"`
	ReactEmoji     string        `yaml:"react_emoji"`
	GroupPolicy    string        `yaml:"group_policy"` // "open" | "mention" | "allowlist"
	GroupAllowFrom []string      `yaml:"group_allow_from"`
	DM             SlackDMConfig `yaml:"dm"`
	AllowFrom      []string      `yaml:"allow_from"`
}

type WhatsAppConfig struct {
	Enabled     bool     `yaml:"enabled"`
	BridgeURL   string   `yaml:"bridge_url"`
	BridgeToken string   `yaml:"bridge_token"`
	AllowFrom   []string `yaml:"allow_from"`
}

type FeishuConfig struct {
	Enabled           bool     `yaml:"enabled"`
	AppID             string   `yaml:"app_id"`
	AppSecret         string   `yaml:"app_secret"`
	EncryptKey        string   `yaml:"encrypt_key"`
	VerificationToken string   `yaml:"verification_token"`
	AllowFrom         []string `yaml:"allow_from"`
}

type DingTalkConfig struct {
	Enabled      bool     `yaml:"enabled"`
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	AllowFrom    []string `yaml:"allow_from"`
}

type EmailConfig struct {
	Enabled             bool     `yaml:"enabled"`
	ConsentGranted      bool     `yaml:"consent_granted"`
	IMAPHost            string   `yaml:"imap_host"`
	IMAPPort            int      `yaml:"imap_port"`
	IMAPUsername        string   `yaml:"imap_username"`
	IMAPPassword        string   `yaml:"imap_password"`
	SMTPHost            string   `yaml:"smtp_host"`
	SMTPPort            int      `yaml:"smtp_port"`
	SMTPUsername        string   `yaml:"smtp_username"`
	SMTPPassword        string   `yaml:"smtp_password"`
	FromAddress         string   `yaml:"from_address"`
	AutoReplyEnabled    bool     `yaml:"auto_reply_enabled"`
	PollIntervalSeconds int      `yaml:"poll_interval_seconds"`
	MarkSeen            bool     `yaml:"mark_seen"`
	MaxBodyChars        int      `yaml:"max_body_chars"`
	SubjectPrefix       string   `yaml:"subject_prefix"`
	AllowFrom           []string `yaml:"allow_from"`
}

type QQConfig struct {
	Enabled   bool     `yaml:"enabled"`
	AppID     string   `yaml:"app_id"`
	Secret    string   `yaml:"secret"`
	AllowFrom []string `yaml:"allow_from"`
}

// MochatConfig Mochat 渠道配置
type MochatConfig struct {
	Enabled bool   `yaml:"enabled"`
	BaseURL string `yaml:"base_url"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:         "~/.nanobot/workspace",
				Model:             "anthropic/claude-opus-4-5",
				MaxTokens:         8192,
				Temperature:       0.1,
				MaxToolIterations: 40,
				MemoryWindow:      100,
			},
		},
		Gateway: GatewayConfig{
			Host: "0.0.0.0",
			Port: 18790,
			Heartbeat: HeartbeatConfig{
				Enabled:   true,
				IntervalS: 1800,
			},
		},
		Tools: ToolsConfig{
			Web: WebToolsConfig{
				Search: WebSearchConfig{MaxResults: 5},
			},
			Exec: ExecToolConfig{Timeout: 60},
		},
	}
}

// WorkspacePath 返回展开后的 workspace 路径
func (c *Config) WorkspacePath() string {
	ws := c.Agents.Defaults.Workspace
	if len(ws) > 0 && ws[0] == '~' {
		// 展开 ~ 为 home 目录
		home := ""
		if h, err := filepath.Abs(filepath.Join("~")); err == nil {
			home = h
		}
		ws = filepath.Join(home, ws[1:])
	}
	return ws
}

// GetProvider 根据 model 名称匹配 Provider 配置
func (c *Config) GetProvider(model string) *ProviderConfig {
	p, _ := c.matchProvider(model)
	return p
}

// GetProviderName 获取匹配的 Provider 名称
func (c *Config) GetProviderName(model string) string {
	_, name := c.matchProvider(model)
	return name
}

// GetProviderByName 根据 provider 名称直接获取配置指针
func (c *Config) GetProviderByName(name string) *ProviderConfig {
	m := c.providerMap()
	return m[name]
}

// providerMap 返回 name → *ProviderConfig 映射（schema 级别，供其他方法调用）
func (c *Config) providerMap() map[string]*ProviderConfig {
	return map[string]*ProviderConfig{
		"custom":      &c.Providers.Custom,
		"anthropic":   &c.Providers.Anthropic,
		"openai":      &c.Providers.OpenAI,
		"openrouter":  &c.Providers.OpenRouter,
		"deepseek":    &c.Providers.DeepSeek,
		"groq":        &c.Providers.Groq,
		"zhipu":       &c.Providers.Zhipu,
		"dashscope":   &c.Providers.DashScope,
		"vllm":        &c.Providers.VLLM,
		"gemini":      &c.Providers.Gemini,
		"moonshot":    &c.Providers.Moonshot,
		"minimax":     &c.Providers.MiniMax,
		"aihubmix":    &c.Providers.AiHubMix,
		"siliconflow": &c.Providers.SiliconFlow,
		"volcengine":  &c.Providers.VolcEngine,
	}
}
