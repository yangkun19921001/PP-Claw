package config

import "path/filepath"

// Config 根配置 (对标 pp-claw/config/schema.py:Config)
type Config struct {
	Agents    AgentsConfig    `yaml:"agents"`
	Channels  ChannelsConfig  `yaml:"channels"`
	Providers ProvidersConfig `yaml:"providers"`
	Gateway   GatewayConfig   `yaml:"gateway"`
	Tools     ToolsConfig     `yaml:"tools"`
}

// AgentsConfig Agent 配置
type AgentsConfig struct {
	Defaults AgentDefaults  `yaml:"defaults"`
	List     []AgentEntry   `yaml:"list,omitempty"`
	Bindings []BindingEntry `yaml:"bindings,omitempty"`
}

// AgentEntry 定义一个命名 Agent（声明式配置，不对应运行时实例）
type AgentEntry struct {
	ID                  string  `yaml:"id"`
	Name                string  `yaml:"name,omitempty"`
	Default             bool    `yaml:"default,omitempty"`
	Model               string  `yaml:"model,omitempty"`
	Workspace           string  `yaml:"workspace,omitempty"`
	MaxTokens           int     `yaml:"max_tokens,omitempty"`
	Temperature         float64 `yaml:"temperature,omitempty"`
	MaxToolIterations   int     `yaml:"max_tool_iterations,omitempty"`
	MemoryWindow        int     `yaml:"memory_window,omitempty"`
	ContextWindowTokens int     `yaml:"context_window_tokens,omitempty"`
}

// BindingEntry 路由规则：将消息匹配到目标 Agent
// 评估顺序自上而下，第一个匹配的胜出
type BindingEntry struct {
	AgentID   string   `yaml:"agent_id"`
	Channel   string   `yaml:"channel,omitempty"`
	AccountID string   `yaml:"account_id,omitempty"`
	ChatIDs   []string `yaml:"chat_ids,omitempty"`
	SenderIDs []string `yaml:"sender_ids,omitempty"`
	Default   bool     `yaml:"default,omitempty"`
}

// AgentDefaults 默认 Agent 配置
type AgentDefaults struct {
	Workspace           string  `yaml:"workspace"`
	Model               string  `yaml:"model"`
	MaxTokens           int     `yaml:"max_tokens"`
	Temperature         float64 `yaml:"temperature"`
	MaxToolIterations   int     `yaml:"max_tool_iterations"`
	MemoryWindow        int     `yaml:"memory_window"`
	ContextWindowTokens int     `yaml:"context_window_tokens"` // default 65536
}

// ChannelsConfig 渠道配置
type ChannelsConfig struct {
	SendProgress   bool                 `yaml:"send_progress"`
	SendToolHints  bool                 `yaml:"send_tool_hints"`
	Telegram       TelegramConfig       `yaml:"telegram"`
	Discord        DiscordConfig        `yaml:"discord"`
	Slack          SlackConfig          `yaml:"slack"`
	WhatsApp       WhatsAppConfig       `yaml:"whatsapp"`
	Feishu         FeishuConfig         `yaml:"feishu"`
	DingTalk       DingTalkConfig       `yaml:"dingtalk"`
	Email          EmailConfig          `yaml:"email"`
	QQ             QQConfig             `yaml:"qq"`
	Mochat         MochatConfig         `yaml:"mochat"`
	Wecom          WecomConfig          `yaml:"wecom"`
	Matrix         MatrixConfig         `yaml:"matrix"`
	WechatPersonal WechatPersonalConfig `yaml:"wechat_personal"`
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

// AzureProviderConfig Azure OpenAI Provider 配置
type AzureProviderConfig struct {
	APIKey       string `yaml:"api_key"`
	APIBase      string `yaml:"api_base"`
	DefaultModel string `yaml:"default_model"`
	APIVersion   string `yaml:"api_version"` // default "2024-10-21"
}

// ProvidersConfig 所有 Provider 配置
type ProvidersConfig struct {
	Custom        ProviderConfig      `yaml:"custom"`
	Anthropic     ProviderConfig      `yaml:"anthropic"`
	OpenAI        ProviderConfig      `yaml:"openai"`
	OpenRouter    ProviderConfig      `yaml:"openrouter"`
	DeepSeek      ProviderConfig      `yaml:"deepseek"`
	Groq          ProviderConfig      `yaml:"groq"`
	Zhipu         ProviderConfig      `yaml:"zhipu"`
	DashScope     ProviderConfig      `yaml:"dashscope"`
	VLLM          ProviderConfig      `yaml:"vllm"`
	Gemini        ProviderConfig      `yaml:"gemini"`
	Ollama        ProviderConfig      `yaml:"ollama"`
	Moonshot      ProviderConfig      `yaml:"moonshot"`
	MiniMax       ProviderConfig      `yaml:"minimax"`
	AiHubMix      ProviderConfig      `yaml:"aihubmix"`
	SiliconFlow   ProviderConfig      `yaml:"siliconflow"`
	VolcEngine    ProviderConfig      `yaml:"volcengine"`
	OpenAICodex   ProviderConfig      `yaml:"openai_codex"`
	GithubCopilot ProviderConfig      `yaml:"github_copilot"`
	AzureOpenAI   AzureProviderConfig `yaml:"azure_openai"`
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

// ============ 渠道子配置 ============
// 每个渠道拆分为 AccountConfig（账号级凭证/行为）+ Config（顶层开关 + 多账号支持）
// 顶层字段通过 yaml:",inline" 嵌入，作为默认账号配置；accounts map 中的条目会 shallow merge 覆盖

// --- Telegram ---

type TelegramAccountConfig struct {
	Token          string   `yaml:"token"`
	AllowFrom      []string `yaml:"allow_from"`
	Proxy          string   `yaml:"proxy"`
	ReplyToMessage bool     `yaml:"reply_to_message"`
}

type TelegramConfig struct {
	Enabled                bool                                  `yaml:"enabled"`
	DefaultAccount         string                                `yaml:"default_account,omitempty"`
	Accounts               map[string]TelegramAccountConfig      `yaml:"accounts,omitempty"`
	TelegramAccountConfig  `yaml:",inline"`
}

// --- Discord ---

type DiscordAccountConfig struct {
	Token      string   `yaml:"token"`
	AllowFrom  []string `yaml:"allow_from"`
	GatewayURL string   `yaml:"gateway_url"`
	Intents    int      `yaml:"intents"`
}

type DiscordConfig struct {
	Enabled               bool                                 `yaml:"enabled"`
	DefaultAccount        string                               `yaml:"default_account,omitempty"`
	Accounts              map[string]DiscordAccountConfig      `yaml:"accounts,omitempty"`
	DiscordAccountConfig  `yaml:",inline"`
}

// --- Slack ---

// SlackDMConfig Slack DM 策略
type SlackDMConfig struct {
	Enabled   bool     `yaml:"enabled"`
	Policy    string   `yaml:"policy"` // "open" | "allowlist"
	AllowFrom []string `yaml:"allow_from"`
}

type SlackAccountConfig struct {
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

type SlackConfig struct {
	Enabled             bool                              `yaml:"enabled"`
	DefaultAccount      string                            `yaml:"default_account,omitempty"`
	Accounts            map[string]SlackAccountConfig     `yaml:"accounts,omitempty"`
	SlackAccountConfig  `yaml:",inline"`
}

// --- WhatsApp ---

type WhatsAppAccountConfig struct {
	BridgeURL   string   `yaml:"bridge_url"`
	BridgeToken string   `yaml:"bridge_token"`
	AllowFrom   []string `yaml:"allow_from"`
}

type WhatsAppConfig struct {
	Enabled                bool                                  `yaml:"enabled"`
	DefaultAccount         string                                `yaml:"default_account,omitempty"`
	Accounts               map[string]WhatsAppAccountConfig      `yaml:"accounts,omitempty"`
	WhatsAppAccountConfig  `yaml:",inline"`
}

// --- Feishu ---

type FeishuAccountConfig struct {
	AppID               string   `yaml:"app_id"`
	AppSecret           string   `yaml:"app_secret"`
	EncryptKey          string   `yaml:"encrypt_key"`
	VerificationToken   string   `yaml:"verification_token"`
	AllowFrom           []string `yaml:"allow_from"`
	GroupPolicy         string   `yaml:"group_policy"`            // "open"/"mention", default "mention"
	ReactEmoji          string   `yaml:"react_emoji"`             // default "THUMBSUP"
	ReplyToMessage      bool     `yaml:"reply_to_message"`        // reply to the original message
	WikiEnabled         bool     `yaml:"wiki_enabled"`
	DocsEnabled         bool     `yaml:"docs_enabled"`
	OAuthRedirectURL    string   `yaml:"oauth_redirect_url"`      // OAuth 回调地址
	SearchMaxResults    int      `yaml:"search_max_results"`      // 搜索后自动读取的文档数量，默认 3
	AilyAppID           string   `yaml:"aily_app_id"`             // 飞书智能伙伴 App ID
	AilyDataAssetIDs    []string `yaml:"aily_data_asset_ids"`     // Aily 数据知识 ID 列表
	AilyDataAssetTagIDs []string `yaml:"aily_data_asset_tag_ids"` // Aily 数据知识分类 ID 列表
}

type FeishuConfig struct {
	Enabled              bool                                `yaml:"enabled"`
	DefaultAccount       string                              `yaml:"default_account,omitempty"`
	Accounts             map[string]FeishuAccountConfig      `yaml:"accounts,omitempty"`
	FeishuAccountConfig  `yaml:",inline"`
}

// --- DingTalk ---

type DingTalkAccountConfig struct {
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	AllowFrom    []string `yaml:"allow_from"`
}

type DingTalkConfig struct {
	Enabled                bool                                  `yaml:"enabled"`
	DefaultAccount         string                                `yaml:"default_account,omitempty"`
	Accounts               map[string]DingTalkAccountConfig      `yaml:"accounts,omitempty"`
	DingTalkAccountConfig  `yaml:",inline"`
}

// --- Email ---

type EmailAccountConfig struct {
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

type EmailConfig struct {
	Enabled              bool                              `yaml:"enabled"`
	DefaultAccount       string                            `yaml:"default_account,omitempty"`
	Accounts             map[string]EmailAccountConfig     `yaml:"accounts,omitempty"`
	EmailAccountConfig   `yaml:",inline"`
}

// --- QQ ---

type QQAccountConfig struct {
	AppID     string   `yaml:"app_id"`
	Secret    string   `yaml:"secret"`
	AllowFrom []string `yaml:"allow_from"`
}

type QQConfig struct {
	Enabled          bool                           `yaml:"enabled"`
	DefaultAccount   string                         `yaml:"default_account,omitempty"`
	Accounts         map[string]QQAccountConfig     `yaml:"accounts,omitempty"`
	QQAccountConfig  `yaml:",inline"`
}

// --- Mochat ---

type MochatAccountConfig struct {
	BaseURL string `yaml:"base_url"`
}

type MochatConfig struct {
	Enabled              bool                                `yaml:"enabled"`
	DefaultAccount       string                              `yaml:"default_account,omitempty"`
	Accounts             map[string]MochatAccountConfig      `yaml:"accounts,omitempty"`
	MochatAccountConfig  `yaml:",inline"`
}

// --- Wecom ---

type WecomAccountConfig struct {
	BotID          string   `yaml:"bot_id"`
	Secret         string   `yaml:"secret"`
	WelcomeMessage string   `yaml:"welcome_message"`
	AllowFrom      []string `yaml:"allow_from"`
}

type WecomConfig struct {
	Enabled              bool                               `yaml:"enabled"`
	DefaultAccount       string                             `yaml:"default_account,omitempty"`
	Accounts             map[string]WecomAccountConfig      `yaml:"accounts,omitempty"`
	WecomAccountConfig   `yaml:",inline"`
}

// --- Matrix ---

type MatrixAccountConfig struct {
	Homeserver     string   `yaml:"homeserver"`
	AccessToken    string   `yaml:"access_token"`
	UserID         string   `yaml:"user_id"`
	DeviceID       string   `yaml:"device_id"`
	E2EEEnabled    bool     `yaml:"e2ee_enabled"`
	MaxMediaBytes  int      `yaml:"max_media_bytes"`
	AllowFrom      []string `yaml:"allow_from"`
	GroupPolicy    string   `yaml:"group_policy"` // "open"/"mention"/"allowlist"
	GroupAllowFrom []string `yaml:"group_allow_from"`
}

type MatrixConfig struct {
	Enabled              bool                                `yaml:"enabled"`
	DefaultAccount       string                              `yaml:"default_account,omitempty"`
	Accounts             map[string]MatrixAccountConfig      `yaml:"accounts,omitempty"`
	MatrixAccountConfig  `yaml:",inline"`
}

// --- WechatPersonal ---
// 保持内部多账号模式不变，仅添加 DefaultAccount 字段

type WechatPersonalConfig struct {
	Enabled             bool                                   `yaml:"enabled"`
	DefaultAccount      string                                 `yaml:"default_account,omitempty"`
	StateDir            string                                 `yaml:"state_dir"`
	BaseURL             string                                 `yaml:"base_url"`
	CDNBaseURL          string                                 `yaml:"cdn_base_url"`
	BotType             string                                 `yaml:"bot_type"`
	PollTimeoutMS       int                                    `yaml:"poll_timeout_ms"`
	LoginTimeoutS       int                                    `yaml:"login_timeout_s"`
	SessionPauseMinutes int                                    `yaml:"session_pause_minutes"`
	RequestTimeoutMS    int                                    `yaml:"request_timeout_ms"`
	ConfigTimeoutMS     int                                    `yaml:"config_timeout_ms"`
	AllowFrom           []string                               `yaml:"allow_from"`
	Accounts            map[string]WechatPersonalAccountConfig `yaml:"accounts"`
}

// WechatPersonalAccountConfig 单个微信账号配置
type WechatPersonalAccountConfig struct {
	Enabled     bool     `yaml:"enabled"`
	BaseURL     string   `yaml:"base_url"`
	CDNBaseURL  string   `yaml:"cdn_base_url"`
	BotType     string   `yaml:"bot_type"`
	AllowFrom   []string `yaml:"allow_from"`
	ILinkUserID string   `yaml:"ilink_user_id"`
}

// GetEnabledMap 返回 channel_name → enabled 映射，用于动态发现
func (c *ChannelsConfig) GetEnabledMap() map[string]bool {
	return map[string]bool{
		"telegram":        c.Telegram.Enabled,
		"discord":         c.Discord.Enabled,
		"slack":           c.Slack.Enabled,
		"feishu":          c.Feishu.Enabled,
		"dingtalk":        c.DingTalk.Enabled,
		"whatsapp":        c.WhatsApp.Enabled,
		"email":           c.Email.Enabled,
		"qq":              c.QQ.Enabled,
		"mochat":          c.Mochat.Enabled,
		"wecom":           c.Wecom.Enabled,
		"matrix":          c.Matrix.Enabled,
		"wechat_personal": c.WechatPersonal.Enabled,
	}
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:           "~/.pp-claw/workspace",
				Model:               "anthropic/claude-opus-4-5",
				MaxTokens:           8192,
				Temperature:         0.1,
				MaxToolIterations:   40,
				MemoryWindow:        100,
				ContextWindowTokens: 65536,
			},
		},
		Channels: ChannelsConfig{
			SendProgress:  true,
			SendToolHints: true,
			Feishu: FeishuConfig{
				FeishuAccountConfig: FeishuAccountConfig{
					ReplyToMessage: true,
				},
			},
			WechatPersonal: WechatPersonalConfig{
				BaseURL:             "https://ilinkai.weixin.qq.com",
				CDNBaseURL:          "https://novac2c.cdn.weixin.qq.com/c2c",
				BotType:             "3",
				PollTimeoutMS:       35000,
				LoginTimeoutS:       480,
				SessionPauseMinutes: 60,
				RequestTimeoutMS:    15000,
				ConfigTimeoutMS:     10000,
				Accounts:            map[string]WechatPersonalAccountConfig{},
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
		"custom":         &c.Providers.Custom,
		"anthropic":      &c.Providers.Anthropic,
		"openai":         &c.Providers.OpenAI,
		"openrouter":     &c.Providers.OpenRouter,
		"deepseek":       &c.Providers.DeepSeek,
		"groq":           &c.Providers.Groq,
		"zhipu":          &c.Providers.Zhipu,
		"dashscope":      &c.Providers.DashScope,
		"vllm":           &c.Providers.VLLM,
		"gemini":         &c.Providers.Gemini,
		"ollama":         &c.Providers.Ollama,
		"moonshot":       &c.Providers.Moonshot,
		"minimax":        &c.Providers.MiniMax,
		"aihubmix":       &c.Providers.AiHubMix,
		"siliconflow":    &c.Providers.SiliconFlow,
		"volcengine":     &c.Providers.VolcEngine,
		"openai_codex":   &c.Providers.OpenAICodex,
		"github_copilot": &c.Providers.GithubCopilot,
	}
}
