package agent

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/yangkun19921001/PP-Claw/agent/tools"
	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/config"
	"github.com/yangkun19921001/PP-Claw/cron"
	"github.com/yangkun19921001/PP-Claw/providers"
	"github.com/yangkun19921001/PP-Claw/session"
	"go.uber.org/zap"
)

// AgentEnv 一个逻辑 Agent 的全部隔离资源，懒初始化后缓存复用。
type AgentEnv struct {
	AgentID       string
	Workspace     string
	ModelName     string
	MaxIterations int
	MemoryWindow  int

	Context    *ContextBuilder
	Sessions   *session.Manager
	Tools      *tools.Registry
	Subagents  *SubagentManager
	Memory     *MemoryStore
	MCPManager *tools.MCPManager

	ChatModel einomodel.ToolCallingChatModel
	ADKAgent  adk.Agent
	ADKRunner *adk.Runner

	ConsolidateMu sync.Mutex
	ActiveTasks   sync.Map // sessionKey -> []context.CancelFunc

	// Per-session 串行锁
	sessionLocks sync.Map // sessionKey -> *sync.Mutex
}

// AcquireSession 获取 per-session mutex（不存在则创建）
func (env *AgentEnv) AcquireSession(sessionKey string) *sync.Mutex {
	val, _ := env.sessionLocks.LoadOrStore(sessionKey, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// InitEinoADK 初始化 Eino ADK Agent + Runner
func (env *AgentEnv) InitEinoADK(ctx context.Context) error {
	einoTools := env.Tools.ToEinoTools()
	toolsConfig := adk.ToolsConfig{}
	toolsConfig.Tools = einoTools

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "pp-claw-" + env.AgentID,
		Description:   "A helpful AI assistant",
		Model:         env.ChatModel,
		ToolsConfig:   toolsConfig,
		MaxIterations: env.MaxIterations,
	})
	if err != nil {
		return fmt.Errorf("创建 ChatModelAgent 失败: %w", err)
	}
	env.ADKAgent = agent
	env.ADKRunner = adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	return nil
}

// ConnectMCP 连接 MCP 服务器并重建 ADK Runner
func (env *AgentEnv) ConnectMCP(ctx context.Context, cfg *config.Config) {
	if env.MCPManager.IsConnected() || len(cfg.Tools.MCPServers) == 0 {
		return
	}

	servers := make(map[string]tools.MCPServerConfig)
	for name, sc := range cfg.Tools.MCPServers {
		servers[name] = tools.MCPServerConfig{
			Command:     sc.Command,
			Args:        sc.Args,
			Env:         sc.Env,
			URL:         sc.URL,
			Headers:     sc.Headers,
			ToolTimeout: sc.ToolTimeout,
		}
	}

	if err := env.MCPManager.Connect(ctx, servers, env.Tools); err != nil {
		return
	}

	// MCP 工具注册后重建 ADK Runner
	if len(cfg.Tools.MCPServers) > 0 {
		_ = env.InitEinoADK(ctx)
	}
}

// CloseMCP 关闭所有 MCP 连接
func (env *AgentEnv) CloseMCP() {
	if env.MCPManager != nil {
		env.MCPManager.Close()
	}
}

// RegisterDefaultTools 注册默认工具 (从 AgentLoop.registerDefaultTools 提取)
func (env *AgentEnv) RegisterDefaultTools(cfg *config.Config, msgBus *bus.MessageBus, cronService *cron.Service, logger *zap.Logger) {
	allowedDir := ""
	if cfg.Tools.RestrictToWorkspace {
		allowedDir = env.Workspace
	}

	env.Tools.Register(&tools.ReadFileTool{Workspace: env.Workspace, AllowedDir: allowedDir})
	env.Tools.Register(&tools.WriteFileTool{Workspace: env.Workspace, AllowedDir: allowedDir})
	env.Tools.Register(&tools.EditFileTool{Workspace: env.Workspace, AllowedDir: allowedDir})
	env.Tools.Register(&tools.ListDirTool{Workspace: env.Workspace, AllowedDir: allowedDir})

	env.Tools.Register(&tools.ExecTool{
		WorkingDir:          env.Workspace,
		Timeout:             cfg.Tools.Exec.Timeout,
		RestrictToWorkspace: cfg.Tools.RestrictToWorkspace,
	})

	env.Tools.Register(&tools.WebSearchTool{
		APIKey:     cfg.Tools.Web.Search.APIKey,
		MaxResults: cfg.Tools.Web.Search.MaxResults,
	})
	env.Tools.Register(&tools.WebFetchTool{MaxChars: 50000})

	env.Tools.Register(&tools.MessageTool{
		SendCallback: msgBus.PublishOutbound,
		SendWithContext: func(ctx context.Context, msg *bus.OutboundMessage) {
			msgBus.PublishOutbound(msg)
		},
	})

	env.Tools.Register(&tools.SpawnTool{
		SpawnFunc: func(ctx context.Context, task, label, channel, accountID, chatID string) string {
			return env.Subagents.Spawn(ctx, task, label, channel, accountID, chatID)
		},
	})

	if cronService != nil {
		env.Tools.Register(&tools.CronTool{CronService: cronService})
	}

	if cfg.Channels.Feishu.Enabled && (cfg.Channels.Feishu.WikiEnabled || cfg.Channels.Feishu.DocsEnabled || cfg.Channels.Feishu.AilyAppID != "") {
		searchMax := cfg.Channels.Feishu.SearchMaxResults
		if searchMax <= 0 {
			searchMax = 3
		}
		oauthRedirect := cfg.Channels.Feishu.OAuthRedirectURL
		if oauthRedirect == "" && cfg.Gateway.Port > 0 {
			oauthRedirect = fmt.Sprintf("http://localhost:%d/feishu/oauth/callback", cfg.Gateway.Port)
		}
		feishuTools := tools.CreateFeishuTools(&tools.FeishuToolsConfig{
			AppID:               cfg.Channels.Feishu.AppID,
			AppSecret:           cfg.Channels.Feishu.AppSecret,
			OAuthRedirectURL:    oauthRedirect,
			SearchMaxResults:    searchMax,
			Logger:              logger,
			AilyAppID:           cfg.Channels.Feishu.AilyAppID,
			AilyDataAssetIDs:    cfg.Channels.Feishu.AilyDataAssetIDs,
			AilyDataAssetTagIDs: cfg.Channels.Feishu.AilyDataAssetTagIDs,
		})
		for _, ft := range feishuTools {
			env.Tools.Register(ft)
		}
	}
}

// SetToolContext 更新工具上下文
func (env *AgentEnv) SetToolContext(channel, accountID, chatID string) {
	for _, name := range env.Tools.Names() {
		t := env.Tools.Get(name)
		if setter, ok := t.(tools.AccountContextSetter); ok {
			setter.SetContextWithAccount(channel, accountID, chatID)
			continue
		}
		if setter, ok := t.(tools.ContextSetter); ok {
			setter.SetContext(channel, chatID)
		}
	}
}

// AgentEnvPool 管理多个 AgentEnv 的懒创建和缓存。
type AgentEnvPool struct {
	cfg         *config.Config
	bus         *bus.MessageBus
	logger      *zap.Logger
	cronService *cron.Service

	mu   sync.RWMutex
	envs map[string]*AgentEnv
}

// NewAgentEnvPool 创建 Agent 环境池。
func NewAgentEnvPool(cfg *config.Config, msgBus *bus.MessageBus, logger *zap.Logger, cronService *cron.Service) *AgentEnvPool {
	return &AgentEnvPool{
		cfg:         cfg,
		bus:         msgBus,
		logger:      logger,
		cronService: cronService,
		envs:        make(map[string]*AgentEnv),
	}
}

// GetOrCreate 根据 agentID 获取或懒创建 AgentEnv。
func (p *AgentEnvPool) GetOrCreate(agentID string) (*AgentEnv, error) {
	// 快速路径: 读锁查缓存
	p.mu.RLock()
	env, ok := p.envs[agentID]
	p.mu.RUnlock()
	if ok {
		return env, nil
	}

	// 慢路径: 写锁 + 二次检查 + 创建
	p.mu.Lock()
	defer p.mu.Unlock()

	if env, ok = p.envs[agentID]; ok {
		return env, nil
	}

	env, err := p.createEnv(agentID)
	if err != nil {
		return nil, err
	}
	p.envs[agentID] = env
	return env, nil
}

func (p *AgentEnvPool) createEnv(agentID string) (*AgentEnv, error) {
	agentDefaults := p.cfg.ResolveAgentDefaults(agentID)
	workspace := p.cfg.ResolveAgentWorkspace(agentID)

	// 确保 workspace 目录存在
	if err := os.MkdirAll(workspace, 0755); err != nil {
		return nil, fmt.Errorf("创建 agent %q workspace 目录失败: %w", agentID, err)
	}

	// 创建 ChatModel
	chatModel, err := providers.NewChatModel(p.logger, p.cfg, agentDefaults.Model)
	if err != nil {
		return nil, fmt.Errorf("创建 agent %q ChatModel 失败: %w", agentID, err)
	}

	env := &AgentEnv{
		AgentID:       agentID,
		Workspace:     workspace,
		ModelName:     agentDefaults.Model,
		MaxIterations: agentDefaults.MaxToolIterations,
		MemoryWindow:  agentDefaults.MemoryWindow,
		Context:       NewContextBuilder(workspace),
		Sessions:      session.NewManager(workspace),
		Tools:         tools.NewRegistry(p.logger),
		Subagents:     NewSubagentManager(workspace, p.bus, agentDefaults.Model, chatModel, p.logger),
		Memory:        NewMemoryStore(workspace, chatModel, p.logger),
		MCPManager:    tools.NewMCPManager(p.logger),
		ChatModel:     chatModel,
	}

	// 注册默认工具
	env.RegisterDefaultTools(p.cfg, p.bus, p.cronService, p.logger)

	// 初始化 ADK
	ctx := context.Background()
	if err := env.InitEinoADK(ctx); err != nil {
		return nil, fmt.Errorf("初始化 agent %q ADK 失败: %w", agentID, err)
	}

	p.logger.Info("AgentEnv 创建完成",
		zap.String("agent_id", agentID),
		zap.String("workspace", workspace),
		zap.String("model", agentDefaults.Model),
		zap.Int("max_iterations", agentDefaults.MaxToolIterations),
		zap.Int("tools", len(env.Tools.Names())),
	)

	return env, nil
}

// Close 关闭所有 AgentEnv 的资源。
func (p *AgentEnvPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, env := range p.envs {
		env.CloseMCP()
		p.logger.Info("AgentEnv 已关闭", zap.String("agent_id", id))
	}
}

// GetFeishuOAuthHandler 遍历所有 env 查找飞书 OAuth handler。
func (p *AgentEnvPool) GetFeishuOAuthHandler() any {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, env := range p.envs {
		if t := env.Tools.Get("feishu_wiki"); t != nil {
			if wt, ok := t.(*tools.FeishuWikiTool); ok && wt.TokenManager != nil {
				return wt.TokenManager
			}
		}
	}
	return nil
}
