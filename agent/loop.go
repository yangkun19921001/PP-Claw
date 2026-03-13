package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/yangkun19921001/PP-Claw/agent/tools"
	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/config"
	"github.com/yangkun19921001/PP-Claw/cron"
	"github.com/yangkun19921001/PP-Claw/session"
	"go.uber.org/zap"
)

// AgentLoop Agent 循环 (对标 pp-claw/agent/loop.py:AgentLoop)
type AgentLoop struct {
	bus           *bus.MessageBus
	cfg           *config.Config
	workspace     string
	model         string
	maxIterations int
	memoryWindow  int
	logger        *zap.Logger

	context    *ContextBuilder
	sessions   *session.Manager
	tools      *tools.Registry
	subagents  *SubagentManager
	memory     *MemoryStore
	mcpManager *tools.MCPManager

	// Eino ADK
	chatModel einomodel.ToolCallingChatModel
	adkAgent  adk.Agent
	adkRunner *adk.Runner

	running       bool
	consolidateMu sync.Mutex // 防止并发合并同一 session
	cronService   *cron.Service
}

// AgentLoopConfig 循环配置
type AgentLoopConfig struct {
	Bus         *bus.MessageBus
	Config      *config.Config
	Workspace   string
	Logger      *zap.Logger
	Sessions    *session.Manager
	ChatModel   einomodel.ToolCallingChatModel
	CronService *cron.Service
}

// NewAgentLoop 创建 Agent 循环
func NewAgentLoop(cfg *AgentLoopConfig) (*AgentLoop, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	workspace := cfg.Workspace
	agentCfg := cfg.Config.Agents.Defaults

	loop := &AgentLoop{
		bus:           cfg.Bus,
		cfg:           cfg.Config,
		workspace:     workspace,
		model:         agentCfg.Model,
		maxIterations: agentCfg.MaxToolIterations,
		memoryWindow:  agentCfg.MemoryWindow,
		logger:        logger,
		context:       NewContextBuilder(workspace),
		sessions:      cfg.Sessions,
		tools:         tools.NewRegistry(logger),
		subagents:     NewSubagentManager(workspace, cfg.Bus, agentCfg.Model, logger),
		memory:        NewMemoryStore(workspace, cfg.ChatModel, logger),
		mcpManager:    tools.NewMCPManager(logger),
		chatModel:     cfg.ChatModel,
		cronService:   cfg.CronService,
	}

	// 注册默认工具
	loop.registerDefaultTools()

	// 创建 Eino ADK Agent + Runner
	ctx := context.Background()
	if err := loop.initEinoADK(ctx); err != nil {
		return nil, fmt.Errorf("初始化 Eino ADK 失败: %w", err)
	}

	logger.Info("Agent loop 初始化完成",
		zap.String("workspace", workspace),
		zap.String("model", loop.model),
		zap.Int("max_iterations", loop.maxIterations),
		zap.Int("tools", len(loop.tools.Names())),
	)

	return loop, nil
}

// registerDefaultTools 注册默认工具 (对标 loop.py:_register_default_tools)
func (l *AgentLoop) registerDefaultTools() {
	allowedDir := ""
	if l.cfg.Tools.RestrictToWorkspace {
		allowedDir = l.workspace
	}

	// 文件工具
	l.tools.Register(&tools.ReadFileTool{Workspace: l.workspace, AllowedDir: allowedDir})
	l.tools.Register(&tools.WriteFileTool{Workspace: l.workspace, AllowedDir: allowedDir})
	l.tools.Register(&tools.EditFileTool{Workspace: l.workspace, AllowedDir: allowedDir})
	l.tools.Register(&tools.ListDirTool{Workspace: l.workspace, AllowedDir: allowedDir})

	// Shell 工具
	l.tools.Register(&tools.ExecTool{
		WorkingDir:          l.workspace,
		Timeout:             l.cfg.Tools.Exec.Timeout,
		RestrictToWorkspace: l.cfg.Tools.RestrictToWorkspace,
	})

	// Web 工具
	l.tools.Register(&tools.WebSearchTool{
		APIKey:     l.cfg.Tools.Web.Search.APIKey,
		MaxResults: l.cfg.Tools.Web.Search.MaxResults,
	})
	l.tools.Register(&tools.WebFetchTool{MaxChars: 50000})

	// 消息工具
	l.tools.Register(&tools.MessageTool{
		SendCallback: l.bus.PublishOutbound,
	})

	// 子代理工具 (对标 loop.py: spawn tool)
	l.tools.Register(&tools.SpawnTool{
		SpawnFunc: func(ctx context.Context, task, label, channel, chatID string) string {
			return l.subagents.Spawn(ctx, task, label, channel, chatID)
		},
	})

	// 定时任务工具
	if l.cronService != nil {
		l.tools.Register(&tools.CronTool{
			CronService: l.cronService,
		})
	}

	// 飞书知识库和文档工具
	if l.cfg.Channels.Feishu.Enabled && (l.cfg.Channels.Feishu.WikiEnabled || l.cfg.Channels.Feishu.DocsEnabled || l.cfg.Channels.Feishu.AilyAppID != "") {
		searchMax := l.cfg.Channels.Feishu.SearchMaxResults
		if searchMax <= 0 {
			searchMax = 3
		}
		// OAuthRedirectURL 默认使用 gateway 端口
		oauthRedirect := l.cfg.Channels.Feishu.OAuthRedirectURL
		if oauthRedirect == "" && l.cfg.Gateway.Port > 0 {
			oauthRedirect = fmt.Sprintf("http://localhost:%d/feishu/oauth/callback", l.cfg.Gateway.Port)
		}
		feishuTools := tools.CreateFeishuTools(&tools.FeishuToolsConfig{
			AppID:               l.cfg.Channels.Feishu.AppID,
			AppSecret:           l.cfg.Channels.Feishu.AppSecret,
			OAuthRedirectURL:    oauthRedirect,
			SearchMaxResults:    searchMax,
			Logger:              l.logger,
			AilyAppID:           l.cfg.Channels.Feishu.AilyAppID,
			AilyDataAssetIDs:    l.cfg.Channels.Feishu.AilyDataAssetIDs,
			AilyDataAssetTagIDs: l.cfg.Channels.Feishu.AilyDataAssetTagIDs,
		})
		for _, ft := range feishuTools {
			l.tools.Register(ft)
		}
	}
}

// initEinoADK 初始化 Eino ADK Runner
func (l *AgentLoop) initEinoADK(ctx context.Context) error {
	// 将工具转换为 Eino 格式
	einoTools := l.tools.ToEinoTools()

	toolsConfig := adk.ToolsConfig{}
	toolsConfig.Tools = einoTools

	// 创建 ChatModelAgent
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "pp-claw",
		Description:   "A helpful AI assistant",
		Model:         l.chatModel,
		ToolsConfig:   toolsConfig,
		MaxIterations: l.maxIterations,
	})
	if err != nil {
		return fmt.Errorf("创建 ChatModelAgent 失败: %w", err)
	}
	l.adkAgent = agent

	// 创建 Runner
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent: agent,
	})
	l.adkRunner = runner

	return nil
}

// Run 运行 Agent 循环 (对标 loop.py:run)
func (l *AgentLoop) Run(ctx context.Context) error {
	l.running = true
	l.logger.Info("Agent loop started")

	// 连接 MCP 服务器 (对标 loop.py: await self._connect_mcp())
	l.connectMCP(ctx)

	for l.running {
		// 消费入站消息
		msg, err := l.bus.ConsumeInbound(ctx)
		if err != nil {
			if err == context.Canceled {
				return nil
			}
			continue
		}

		// 处理消息
		response, err := l.processMessage(ctx, msg)
		if err != nil {
			l.logger.Error("处理消息失败", zap.Error(err))
			errMsg := bus.NewOutboundMessage(
				msg.Channel, msg.ChatID,
				fmt.Sprintf("Sorry, I encountered an error: %s", err.Error()),
			)
			errMsg.ReplyTo = extractReplyTo(msg)
			l.bus.PublishOutbound(errMsg)
			continue
		}
		if response != nil {
			l.bus.PublishOutbound(response)
		}
	}

	return nil
}

// Stop 停止循环
func (l *AgentLoop) Stop() {
	l.running = false
	l.CloseMCP()
	l.logger.Info("Agent loop stopping")
}

// GetFeishuOAuthHandler 返回飞书 OAuth 回调的 HTTP handler（挂载到 gateway HTTP server）
func (l *AgentLoop) GetFeishuOAuthHandler() http.Handler {
	if t := l.tools.Get("feishu_wiki"); t != nil {
		if wt, ok := t.(*tools.FeishuWikiTool); ok && wt.TokenManager != nil {
			return wt.TokenManager
		}
	}
	return nil
}

// connectMCP 连接 MCP 服务器 (对标 loop.py:_connect_mcp)
func (l *AgentLoop) connectMCP(ctx context.Context) {
	if l.mcpManager.IsConnected() || len(l.cfg.Tools.MCPServers) == 0 {
		return
	}

	// 转换配置类型
	servers := make(map[string]tools.MCPServerConfig)
	for name, cfg := range l.cfg.Tools.MCPServers {
		servers[name] = tools.MCPServerConfig{
			Command:     cfg.Command,
			Args:        cfg.Args,
			Env:         cfg.Env,
			URL:         cfg.URL,
			Headers:     cfg.Headers,
			ToolTimeout: cfg.ToolTimeout,
		}
	}

	if err := l.mcpManager.Connect(ctx, servers, l.tools); err != nil {
		l.logger.Error("MCP 连接失败", zap.Error(err))
	}

	// MCP 工具注册后需重新初始化 ADK Runner (工具列表变化)
	if len(l.cfg.Tools.MCPServers) > 0 {
		if err := l.initEinoADK(ctx); err != nil {
			l.logger.Error("重新初始化 ADK Runner 失败", zap.Error(err))
		}
	}
}

// CloseMCP 关闭所有 MCP 连接 (对标 loop.py:close_mcp)
func (l *AgentLoop) CloseMCP() {
	if l.mcpManager != nil {
		l.mcpManager.Close()
	}
}

// processMessage 处理单条消息 (对标 loop.py:_process_message)
func (l *AgentLoop) processMessage(ctx context.Context, msg *bus.InboundMessage) (*bus.OutboundMessage, error) {
	preview := msg.Content
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	l.logger.Info("处理消息",
		zap.String("channel", msg.Channel),
		zap.String("sender", msg.SenderID),
		zap.String("content", preview),
	)

	// 更新工具上下文
	l.setToolContext(msg.Channel, msg.ChatID)

	// 开始新的对话回复回合
	if t := l.tools.Get("message"); t != nil {
		if mt, ok := t.(*tools.MessageTool); ok {
			mt.StartTurn()
		}
	}

	// 处理斜杠命令
	cmd := strings.TrimSpace(strings.ToLower(msg.Content))
	if cmd == "/new" {
		sessionKey := msg.SessionKey()
		sess := l.sessions.GetOrCreate(sessionKey)
		// 先执行 archive_all 合并再清空
		if len(sess.Messages) > 0 {
			l.consolidateMemory(ctx, sess, true)
		}
		sess.Clear()
		l.sessions.Save(sess)
		return bus.NewOutboundMessage(msg.Channel, msg.ChatID, "New session started."), nil
	}
	if cmd == "/help" {
		return bus.NewOutboundMessage(msg.Channel, msg.ChatID,
			"🦞 pp-claw commands:\n/new — Start a new conversation\n/help — Show available commands"), nil
	}

	// 获取/创建 Session
	sessionKey := msg.SessionKey()
	sess := l.sessions.GetOrCreate(sessionKey)

	// 构建消息上下文
	history := sess.GetHistory(l.memoryWindow)

	// 构建 Eino 消息: system prompt + history + user message
	var einoMsgs []*schema.Message

	// System prompt
	sysPrompt := l.context.BuildSystemPrompt()
	einoMsgs = append(einoMsgs, &schema.Message{
		Role:    schema.System,
		Content: sysPrompt,
	})

	// History
	for _, h := range history {
		role, _ := h["role"].(string)
		content, _ := h["content"].(string)
		var schemaRole schema.RoleType
		switch role {
		case "user":
			schemaRole = schema.User
		case "assistant":
			schemaRole = schema.Assistant
		default:
			continue
		}
		einoMsgs = append(einoMsgs, &schema.Message{Role: schemaRole, Content: content})
	}

	// Current user message with runtime context
	rc := l.context.injectRuntimeContext(msg.Content, msg.Channel, msg.ChatID)
	userContentStr, _ := rc.(string)
	if userContentStr == "" {
		userContentStr = msg.Content
	}
	einoMsgs = append(einoMsgs, &schema.Message{Role: schema.User, Content: userContentStr})

	// 构建 progress 回调
	replyTo := extractReplyTo(msg)
	onProgress := func(content string, toolHint bool) {
		progressMsg := bus.NewOutboundMessage(msg.Channel, msg.ChatID, content)
		progressMsg.ReplyTo = replyTo
		progressMsg.Metadata["_progress"] = true
		if toolHint {
			progressMsg.Metadata["_tool_hint"] = true
		}
		l.bus.PublishOutbound(progressMsg)
	}

	// 通过 ADK Runner 执行
	finalContent, err := l.runWithADK(ctx, einoMsgs, onProgress)
	if err != nil {
		return nil, err
	}

	if finalContent == "" {
		finalContent = "I've completed processing but have no response to give."
	}

	// 日志：最终响应
	responsePreview := finalContent
	if len(responsePreview) > 200 {
		responsePreview = responsePreview[:200] + "..."
	}
	l.logger.Info("🤖 Agent response",
		zap.String("channel", msg.Channel),
		zap.String("chat_id", msg.ChatID),
		zap.Int("length", len(finalContent)),
		zap.String("preview", responsePreview),
	)

	// 保存会话
	sess.AddMessage("user", msg.Content)
	sess.AddMessage("assistant", finalContent)
	l.sessions.Save(sess)

	// 检查是否需要触发记忆合并
	unconsolidated := len(sess.Messages) - sess.LastConsolidated
	if unconsolidated >= l.memoryWindow {
		go l.consolidateMemory(ctx, sess, false)
	}

	// 如果 MessageTool 已经在本轮回合中发送过消息，则不再重复发送
	if t := l.tools.Get("message"); t != nil {
		if mt, ok := t.(*tools.MessageTool); ok && mt.SentInTurn {
			return nil, nil
		}
	}

	out := bus.NewOutboundMessage(msg.Channel, msg.ChatID, finalContent)
	out.ReplyTo = extractReplyTo(msg)
	return out, nil
}

// consolidateMemory 执行记忆合并（带锁防止并发合并同一 session）
func (l *AgentLoop) consolidateMemory(ctx context.Context, sess *session.Session, archiveAll bool) {
	l.consolidateMu.Lock()
	defer l.consolidateMu.Unlock()

	newOffset, ok := l.memory.Consolidate(ctx, sess.Messages, archiveAll, l.memoryWindow, sess.LastConsolidated)
	if ok {
		sess.LastConsolidated = newOffset
		l.sessions.Save(sess)
		l.logger.Info("记忆合并完成", zap.String("session", sess.Key), zap.Int("new_offset", newOffset))
	}
}

// formatToolHint 格式化工具调用提示
func formatToolHint(toolCalls []schema.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}
	tc := toolCalls[0]
	name := tc.Function.Name
	args := tc.Function.Arguments

	// 提取第一个参数值作为预览
	firstArg := ""
	var argsMap map[string]any
	if err := json.Unmarshal([]byte(args), &argsMap); err == nil {
		for _, v := range argsMap {
			if s, ok := v.(string); ok {
				firstArg = s
				break
			}
		}
	}

	if firstArg != "" {
		if len(firstArg) > 40 {
			firstArg = firstArg[:40] + "..."
		}
		return fmt.Sprintf("%s(\"%s\")", name, firstArg)
	}
	return name + "()"
}

// runWithADK 通过 Eino ADK Runner 运行
func (l *AgentLoop) runWithADK(ctx context.Context, messages []*schema.Message, onProgress func(content string, toolHint bool)) (string, error) {
	iter := l.adkRunner.Run(ctx, messages)

	var lastContent string
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return "", fmt.Errorf("agent error: %w", event.Err)
		}

		// 提取 Assistant 消息内容
		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, msgErr := event.Output.MessageOutput.GetMessage()
			if msgErr != nil {
				continue
			}
			if msg == nil {
				continue
			}

			if msg.Role == schema.Assistant {
				content := stripThink(msg.Content)

				// 检测 tool calls → 发送 progress + 日志
				if len(msg.ToolCalls) > 0 {
					for _, tc := range msg.ToolCalls {
						l.logger.Info("🤖 Assistant tool call",
							zap.String("tool", tc.Function.Name),
							zap.String("args", tc.Function.Arguments),
						)
					}
					if onProgress != nil {
						if content != "" {
							onProgress(content, false)
						}
						hint := formatToolHint(msg.ToolCalls)
						if hint != "" {
							onProgress(hint, true)
						}
					}
				}

				if content != "" {
					lastContent = content
				}
			}
		}
	}

	return lastContent, nil
}

// setToolContext 更新工具上下文
func (l *AgentLoop) setToolContext(channel, chatID string) {
	for _, name := range l.tools.Names() {
		t := l.tools.Get(name)
		if setter, ok := t.(tools.ContextSetter); ok {
			setter.SetContext(channel, chatID)
		}
	}
}

// extractReplyTo 从入站消息的 Metadata 中提取 message_id 作为回复目标
// 飞书群聊不使用引用回复（避免消息折叠在"x 条回复"里），直接发送到群聊天流
func extractReplyTo(msg *bus.InboundMessage) string {
	if msg == nil || msg.Metadata == nil {
		return ""
	}
	// // 飞书群聊：直接发到群，不做引用回复
	// if msg.Channel == "feishu" {
	// 	if chatType, _ := msg.Metadata["chat_type"].(string); chatType == "group" {
	// 		return ""
	// 	}
	// }
	if id, ok := msg.Metadata["message_id"]; ok {
		if s, ok := id.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", id)
	}
	return ""
}

// stripThink 移除 <think>...</think> 块
var thinkRe = regexp.MustCompile(`(?s)<think>.*?</think>`)

func stripThink(text string) string {
	if text == "" {
		return ""
	}
	return strings.TrimSpace(thinkRe.ReplaceAllString(text, ""))
}

// ProcessDirect 直接处理消息 (用于 CLI)
func (l *AgentLoop) ProcessDirect(ctx context.Context, content string) (string, error) {
	msg := bus.NewInboundMessage("cli", "user", "direct", content)
	resp, err := l.processMessage(ctx, msg)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return resp.Content, nil
}
