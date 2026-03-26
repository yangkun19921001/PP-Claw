package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

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

// ToolProgressEvent 工具执行进度事件
type ToolProgressEvent struct {
	Kind          string // "thought", "tool_start", "tool_done"
	Content       string
	ToolCallID    string
	ToolName      string
	ToolArgs      string
	DurationMs    int64
	Success       bool
	ResultPreview string
}

type agentTraceContextKey string

const agentTraceIDContextKey agentTraceContextKey = "agent_trace_id"

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

	// Active task tracking for /stop support
	activeTasks sync.Map // session_key -> []context.CancelFunc
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
		subagents:     NewSubagentManager(workspace, cfg.Bus, agentCfg.Model, cfg.ChatModel, logger),
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
		SendWithContext: func(ctx context.Context, msg *bus.OutboundMessage) {
			traceID := traceIDFromContext(ctx)
			l.logger.Info("消息链路: 通过 message 工具发送出站消息",
				outboundLogFields(msg, traceID)...,
			)
			l.bus.PublishOutbound(msg)
		},
	})

	// 子代理工具 (对标 loop.py: spawn tool)
	l.tools.Register(&tools.SpawnTool{
		SpawnFunc: func(ctx context.Context, task, label, channel, accountID, chatID string) string {
			return l.subagents.Spawn(ctx, task, label, channel, accountID, chatID)
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

		traceID := buildMessageTraceID(msg)
		msgCtx := withMessageTrace(ctx, traceID)
		consumeStarted := time.Now()
		inboundFields := inboundLogFields(msg, traceID)
		inboundFields = append(inboundFields,
			zap.Int("inbound_queue_remaining", l.bus.InboundSize()),
		)
		l.logger.Info("消息链路: 收到入站消息", inboundFields...)

		// 处理消息
		response, err := l.processMessage(msgCtx, msg)
		if err != nil {
			l.logger.Error("消息链路: 处理消息失败",
				append(inboundFields,
					zap.Int64("duration_ms", time.Since(consumeStarted).Milliseconds()),
					zap.Error(err),
				)...,
			)
			errMsg := bus.NewOutboundMessage(
				msg.Channel, msg.ChatID,
				fmt.Sprintf("Sorry, I encountered an error: %s", err.Error()),
			)
			errMsg.AccountID = msg.AccountID
			errMsg.ReplyTo = extractReplyTo(msg)
			l.logger.Info("消息链路: 发布错误响应",
				append(
					outboundLogFields(errMsg, traceID),
					zap.Int64("duration_ms", time.Since(consumeStarted).Milliseconds()),
				)...,
			)
			l.bus.PublishOutbound(errMsg)
			l.logger.Info("消息链路: 消息消费结束",
				append(inboundFields,
					zap.String("result", "error_response"),
					zap.Int64("duration_ms", time.Since(consumeStarted).Milliseconds()),
				)...,
			)
			continue
		}
		if response != nil {
			l.logger.Info("消息链路: 发布最终响应",
				append(
					outboundLogFields(response, traceID),
					zap.Int64("duration_ms", time.Since(consumeStarted).Milliseconds()),
				)...,
			)
			l.bus.PublishOutbound(response)
			l.logger.Info("消息链路: 消息消费结束",
				append(inboundFields,
					zap.String("result", "final_response"),
					zap.Int64("duration_ms", time.Since(consumeStarted).Milliseconds()),
				)...,
			)
			continue
		}
		l.logger.Info("消息链路: 消息消费结束",
			append(inboundFields,
				zap.String("result", "no_final_response"),
				zap.Int64("duration_ms", time.Since(consumeStarted).Milliseconds()),
			)...,
		)
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
		} else {
			l.logger.Info("MCP 工具注册完成，ADK 已重建",
				zap.Int("total_tools", len(l.tools.Names())),
				zap.Strings("tools", l.tools.Names()),
			)
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
func (l *AgentLoop) processMessage(ctx context.Context, msg *bus.InboundMessage) (response *bus.OutboundMessage, err error) {
	traceID := traceIDFromContext(ctx)
	if traceID == "" {
		traceID = buildMessageTraceID(msg)
		ctx = withMessageTrace(ctx, traceID)
	}

	started := time.Now()
	result := "processing"
	defer func() {
		fields := append(inboundLogFields(msg, traceID),
			zap.String("result", result),
			zap.Int64("duration_ms", time.Since(started).Milliseconds()),
		)
		if response != nil {
			fields = append(fields,
				zap.Int("response_length", len(response.Content)),
				zap.Int("response_media_count", len(response.Media)),
			)
		}
		if err != nil {
			fields = append(fields, zap.Error(err))
		}
		l.logger.Info("消息链路: processMessage 结束", fields...)
	}()

	l.logger.Info("消息链路: 进入处理流水线", inboundLogFields(msg, traceID)...)

	// 更新工具上下文
	l.setToolContext(msg.Channel, msg.AccountID, msg.ChatID)

	// 开始新的对话回复回合
	replyTo := extractReplyTo(msg)
	if t := l.tools.Get("message"); t != nil {
		if mt, ok := t.(*tools.MessageTool); ok {
			mt.StartTurn()
			mt.SetReplyTo(replyTo)
		}
	}

	// Handle system messages (subagent results, etc.)
	if msg.Channel == "system" {
		return l.handleSystemMessage(ctx, msg)
	}

	// 处理斜杠命令
	cmd := strings.TrimSpace(strings.ToLower(msg.Content))
	if cmd == "/stop" {
		return l.handleStop(msg)
	}
	if cmd == "/new" {
		sessionKey := msg.SessionKey()
		sess := l.sessions.GetOrCreate(sessionKey)
		// 先执行 archive_all 合并再清空
		if len(sess.Messages) > 0 {
			l.consolidateMemory(ctx, sess, true)
		}
		sess.Clear()
		l.sessions.Save(sess)
		result = "command_new"
		response = bus.NewOutboundMessage(msg.Channel, msg.ChatID, "New session started.")
		response.AccountID = msg.AccountID
		return response, nil
	}
	if cmd == "/help" {
		result = "command_help"
		response = bus.NewOutboundMessage(msg.Channel, msg.ChatID,
			"🦞 pp-claw commands:\n/new — Start a new conversation\n/help — Show available commands")
		response.AccountID = msg.AccountID
		return response, nil
	}

	// 获取/创建 Session
	sessionKey := msg.SessionKey()
	sess := l.sessions.GetOrCreate(sessionKey)

	// 构建消息上下文
	history := sess.GetHistory(l.memoryWindow)
	l.logger.Info("消息链路: 会话上下文准备完成",
		zap.String("trace_id", traceID),
		zap.String("session_key", sessionKey),
		zap.Int("history_count", len(history)),
		zap.Int("media_count", len(msg.Media)),
	)

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
	userContent := l.context.buildUserContent(msg.Content, msg.Media)
	rc := l.context.injectRuntimeContext(userContent, msg.Channel, msg.ChatID)
	userContentStr, _ := rc.(string)
	if userContentStr == "" {
		userContentStr = userContent
	}
	einoMsgs = append(einoMsgs, &schema.Message{Role: schema.User, Content: userContentStr})

	l.logger.Info("消息链路: 模型输入构建完成",
		zap.String("trace_id", traceID),
		zap.String("session_key", sessionKey),
		zap.Int("eino_messages", len(einoMsgs)),
		zap.Int("history_count", len(history)),
		zap.Int("attached_media_count", len(msg.Media)),
		zap.Bool("multimodal", false),
		zap.Int("user_content_length", len(userContentStr)),
		zap.Any("message_snapshot", buildModelInputSnapshot(einoMsgs)),
	)

	// 构建 progress 回调（replyTo 已在上方定义）
	onProgress := func(evt ToolProgressEvent) {
		progressMsg := bus.NewOutboundMessage(msg.Channel, msg.ChatID, evt.Content)
		progressMsg.AccountID = msg.AccountID
		progressMsg.ReplyTo = replyTo
		progressMsg.Metadata["_progress"] = true

		switch evt.Kind {
		case "tool_start":
			progressMsg.Metadata["_tool_hint"] = true
			progressMsg.Metadata["_tool_call_id"] = evt.ToolCallID
			progressMsg.Metadata["_tool_name"] = evt.ToolName
			progressMsg.Metadata["_tool_args"] = evt.ToolArgs
			progressMsg.Metadata["_tool_status"] = "running"
		case "tool_done":
			progressMsg.Metadata["_tool_hint"] = true
			progressMsg.Metadata["_tool_call_id"] = evt.ToolCallID
			progressMsg.Metadata["_tool_name"] = evt.ToolName
			progressMsg.Metadata["_tool_args"] = evt.ToolArgs
			progressMsg.Metadata["_tool_status"] = "done"
			progressMsg.Metadata["_tool_duration_ms"] = evt.DurationMs
			progressMsg.Metadata["_tool_result_preview"] = evt.ResultPreview
			if !evt.Success {
				progressMsg.Metadata["_tool_status"] = "error"
			}
		default:
			// "thought" — plain progress text
		}

		progressFields := append(
			outboundLogFields(progressMsg, traceID),
			zap.String("progress_kind", evt.Kind),
		)
		if evt.ToolName != "" {
			progressFields = append(progressFields, zap.String("tool_name", evt.ToolName))
		}
		if evt.ToolCallID != "" {
			progressFields = append(progressFields, zap.String("tool_call_id", evt.ToolCallID))
		}
		l.logger.Info("消息链路: 发布进度消息", progressFields...)
		l.bus.PublishOutbound(progressMsg)
	}

	// 通过 ADK Runner 执行
	l.logger.Info("消息链路: 开始执行 Agent",
		zap.String("trace_id", traceID),
		zap.String("session_key", sessionKey),
	)
	finalContent, finishReason, err := l.runWithADK(ctx, einoMsgs, onProgress)
	if err != nil {
		l.logger.Error("消息链路: 模型调用失败，输出输入快照用于排查",
			zap.String("trace_id", traceID),
			zap.String("session_key", sessionKey),
			zap.Int("eino_messages", len(einoMsgs)),
			zap.Any("message_snapshot", buildModelInputSnapshot(einoMsgs)),
			zap.Error(err),
		)
		result = "agent_error"
		return nil, err
	}
	l.logger.Info("消息链路: Agent 执行完成",
		zap.String("trace_id", traceID),
		zap.String("session_key", sessionKey),
		zap.Int("final_content_length", len(finalContent)),
		zap.String("finish_reason", finishReason),
	)

	if finalContent == "" {
		finalContent = "I've completed processing but have no response to give."
	}

	// 日志：最终响应
	responsePreview := finalContent
	if len(responsePreview) > 200 {
		responsePreview = responsePreview[:200] + "..."
	}
	l.logger.Info("🤖 Agent response",
		zap.String("trace_id", traceID),
		zap.String("channel", msg.Channel),
		zap.String("chat_id", msg.ChatID),
		zap.Int("length", len(finalContent)),
		zap.String("preview", responsePreview),
	)

	// 保存会话: 错误响应不持久化到 session，避免污染对话历史
	if finishReason == "error" {
		l.logger.Warn("消息链路: 错误响应不保存到会话",
			zap.String("trace_id", traceID),
			zap.String("session_key", sessionKey),
		)
	} else {
		l.saveTurn(sess, msg.Content, finalContent)
	}
	l.logger.Info("消息链路: 会话保存完成",
		zap.String("trace_id", traceID),
		zap.String("session_key", sessionKey),
		zap.Int("total_messages", len(sess.Messages)),
	)

	// 检查是否需要触发记忆合并
	unconsolidated := len(sess.Messages) - sess.LastConsolidated
	if unconsolidated >= l.memoryWindow {
		l.logger.Info("消息链路: 触发异步记忆合并",
			zap.String("trace_id", traceID),
			zap.String("session_key", sessionKey),
			zap.Int("unconsolidated", unconsolidated),
			zap.Int("memory_window", l.memoryWindow),
		)
		go l.consolidateMemory(ctx, sess, false)
	}

	// 如果 MessageTool 已经在本轮回合中发送过消息，则不再重复发送
	if t := l.tools.Get("message"); t != nil {
		if mt, ok := t.(*tools.MessageTool); ok && mt.SentInTurn {
			result = "message_tool_sent"
			l.logger.Info("消息链路: 本轮消息已由 message 工具发送，跳过最终出站发布",
				zap.String("trace_id", traceID),
				zap.String("session_key", sessionKey),
			)
			return nil, nil
		}
	}

	out := bus.NewOutboundMessage(msg.Channel, msg.ChatID, finalContent)
	out.AccountID = msg.AccountID
	out.ReplyTo = extractReplyTo(msg)
	result = "final_response_ready"
	response = out
	return response, nil
}

// handleStop cancels all active tasks for the session and subagents.
func (l *AgentLoop) handleStop(msg *bus.InboundMessage) (*bus.OutboundMessage, error) {
	sessionKey := msg.SessionKey()
	cancelled := 0

	// Cancel active agent tasks for this session
	if val, ok := l.activeTasks.Load(sessionKey); ok {
		if cancels, ok := val.([]context.CancelFunc); ok {
			for _, cancel := range cancels {
				cancel()
			}
			cancelled += len(cancels)
		}
		l.activeTasks.Delete(sessionKey)
	}

	// Cancel subagents for this session
	if l.subagents != nil {
		cancelled += l.subagents.CancelBySession(sessionKey)
	}

	text := fmt.Sprintf("Cancelled %d task(s).", cancelled)
	if cancelled == 0 {
		text = "No active tasks to cancel."
	}

	out := bus.NewOutboundMessage(msg.Channel, msg.ChatID, text)
	out.AccountID = msg.AccountID
	return out, nil
}

// trackTask registers a cancel function for the session.
func (l *AgentLoop) trackTask(sessionKey string, cancel context.CancelFunc) {
	val, _ := l.activeTasks.LoadOrStore(sessionKey, []context.CancelFunc{})
	cancels := val.([]context.CancelFunc)
	cancels = append(cancels, cancel)
	l.activeTasks.Store(sessionKey, cancels)
}

// untrackTask removes a cancel function for the session.
func (l *AgentLoop) untrackTask(sessionKey string, cancel context.CancelFunc) {
	val, ok := l.activeTasks.Load(sessionKey)
	if !ok {
		return
	}
	cancels := val.([]context.CancelFunc)
	for i, c := range cancels {
		if &c == &cancel {
			cancels = append(cancels[:i], cancels[i+1:]...)
			break
		}
	}
	if len(cancels) == 0 {
		l.activeTasks.Delete(sessionKey)
	} else {
		l.activeTasks.Store(sessionKey, cancels)
	}
}

// handleSystemMessage processes system channel messages (subagent results, etc.)
func (l *AgentLoop) handleSystemMessage(ctx context.Context, msg *bus.InboundMessage) (*bus.OutboundMessage, error) {
	targetChannel := msg.Channel
	targetAccountID := msg.AccountID
	targetChatID := msg.ChatID
	if targetChannel == "system" {
		if msg.Metadata != nil {
			if v, ok := msg.Metadata["target_channel"].(string); ok && v != "" {
				targetChannel = v
			}
		}
	}
	if targetChannel == "system" {
		parts := strings.SplitN(msg.ChatID, ":", 2)
		if len(parts) != 2 {
			l.logger.Warn("系统消息格式错误", zap.String("chat_id", msg.ChatID))
			return nil, nil
		}
		targetChannel = parts[0]
		targetChatID = parts[1]
	}

	// Determine the role: subagent results as assistant, others as user
	role := "user"
	if msg.SenderID == "subagent" {
		role = "assistant"
	}

	// Get/create session for the target
	sessionKey := targetChannel + ":" + targetChatID
	if targetAccountID != "" {
		sessionKey = targetChannel + ":" + targetAccountID + ":" + targetChatID
	}
	sess := l.sessions.GetOrCreate(sessionKey)

	// Add the system message to session history
	sess.AddMessage(role, msg.Content)
	l.sessions.Save(sess)

	// Build and run through agent loop to generate response
	history := sess.GetHistory(l.memoryWindow)
	var einoMsgs []*schema.Message
	einoMsgs = append(einoMsgs, &schema.Message{
		Role:    schema.System,
		Content: l.context.BuildSystemPrompt(),
	})

	for _, h := range history {
		hRole, _ := h["role"].(string)
		content, _ := h["content"].(string)
		var schemaRole schema.RoleType
		switch hRole {
		case "user":
			schemaRole = schema.User
		case "assistant":
			schemaRole = schema.Assistant
		default:
			continue
		}
		einoMsgs = append(einoMsgs, &schema.Message{Role: schemaRole, Content: content})
	}

	// Ensure last message is user role for the model
	if len(einoMsgs) > 0 && einoMsgs[len(einoMsgs)-1].Role != schema.User {
		einoMsgs = append(einoMsgs, &schema.Message{
			Role:    schema.User,
			Content: "Please summarize the above result for the user.",
		})
	}

	l.setToolContext(targetChannel, targetAccountID, targetChatID)

	finalContent, _, err := l.runWithADK(ctx, einoMsgs, nil)
	if err != nil {
		l.logger.Error("系统消息处理失败", zap.Error(err))
		return nil, nil
	}

	if finalContent == "" {
		return nil, nil
	}

	// Save assistant response
	sess.AddMessage("assistant", finalContent)
	l.sessions.Save(sess)

	// Send to original channel
	out := bus.NewOutboundMessage(targetChannel, targetChatID, finalContent)
	out.AccountID = targetAccountID
	l.bus.PublishOutbound(out)
	return nil, nil
}

// runtimeContextRE matches <runtime_context>...</runtime_context> tags
var runtimeContextRE = regexp.MustCompile(`(?s)<runtime_context>.*?</runtime_context>\s*`)

// base64ImageRE matches base64-encoded image data URIs
var base64ImageRE = regexp.MustCompile(`data:image/[^;]+;base64,[A-Za-z0-9+/=]{100,}`)

// saveTurn saves user+assistant messages to the session with cleanup:
// - Strips runtime context from user messages
// - Replaces base64 images with [image] placeholder
// - Skips empty assistant messages
func (l *AgentLoop) saveTurn(sess *session.Session, userContent, assistantContent string) {
	// Clean user content: remove runtime context
	cleanUser := runtimeContextRE.ReplaceAllString(userContent, "")
	cleanUser = strings.TrimSpace(cleanUser)

	// Replace base64 images
	cleanUser = base64ImageRE.ReplaceAllString(cleanUser, "[image]")

	if cleanUser != "" {
		sess.AddMessage("user", cleanUser)
	}

	// Clean and save assistant content
	cleanAssistant := base64ImageRE.ReplaceAllString(assistantContent, "[image]")
	// Truncate very long assistant messages for session storage
	if len(cleanAssistant) > 16000 {
		cleanAssistant = cleanAssistant[:16000] + "\n[truncated]"
	}

	if cleanAssistant != "" {
		sess.AddMessage("assistant", cleanAssistant)
	}

	l.sessions.Save(sess)
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
// Returns: (content, finishReason, error)
// finishReason: "ok" | "error"
func (l *AgentLoop) runWithADK(ctx context.Context, messages []*schema.Message, onProgress func(ToolProgressEvent)) (string, string, error) {
	iter := l.adkRunner.Run(ctx, messages)
	traceID := traceIDFromContext(ctx)

	var lastContent string
	finishReason := "ok"

	// 死循环检测：记录连续相同（工具名+参数）调用次数
	const maxRepeatErrors = 3
	lastToolSig := ""
	repeatCount := 0

	// 工具开始时间追踪
	toolStartTimes := make(map[string]time.Time)
	// 工具名称追踪（用于 tool_done 事件）
	toolNames := make(map[string]string)
	toolArgs := make(map[string]string)

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return "", "error", fmt.Errorf("agent error: %w", event.Err)
		}

		// 提取消息内容
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		mv := event.Output.MessageOutput
		// 使用 MessageVariant.Role 作为主要角色判断（比内部 msg.Role 更可靠）
		role := mv.Role

		msg, msgErr := mv.GetMessage()
		if msgErr != nil {
			l.logger.Warn("GetMessage error",
				zap.String("trace_id", traceID),
				zap.Error(msgErr),
				zap.String("role", string(role)),
			)
			continue
		}
		if msg == nil {
			continue
		}

		l.logger.Debug("ADK event",
			zap.String("trace_id", traceID),
			zap.String("mv_role", string(role)),
			zap.String("msg_role", string(msg.Role)),
			zap.String("tool_call_id", msg.ToolCallID),
			zap.String("tool_name", msg.ToolName),
			zap.Int("tool_calls", len(msg.ToolCalls)),
		)

		switch role {
		case schema.Assistant:
			content := stripThink(msg.Content)

			// 无 tool_call 时也流式推送文本，避免用户只看到 "Thinking..." 然后突然完成
			if len(msg.ToolCalls) == 0 && content != "" && onProgress != nil {
				onProgress(ToolProgressEvent{
					Kind:    "thought",
					Content: content,
				})
			}

			// 检测 tool calls → 发送 progress + 日志 + 死循环检测
			if len(msg.ToolCalls) > 0 {
				// 构造本轮工具调用签名（用于死循环检测）
				toolSig := ""
				for _, tc := range msg.ToolCalls {
					toolSig += tc.Function.Name + ":" + tc.Function.Arguments + "|"
					l.logger.Info("🤖 Assistant tool call",
						zap.String("trace_id", traceID),
						zap.String("tool", tc.Function.Name),
						zap.String("args", tc.Function.Arguments),
						zap.String("call_id", tc.ID),
					)
					// 记录工具开始时间（使用 tool name 作为 fallback key）
					key := tc.ID
					if key == "" {
						key = tc.Function.Name
					}
					toolStartTimes[key] = time.Now()
					toolNames[key] = tc.Function.Name
					toolArgs[key] = tc.Function.Arguments
				}
				// 死循环检测：连续相同签名超过阈值则强制终止
				if toolSig == lastToolSig {
					repeatCount++
					if repeatCount >= maxRepeatErrors {
						l.logger.Warn("🔄 检测到工具调用死循环，强制终止",
							zap.String("trace_id", traceID),
							zap.String("tool_sig", toolSig),
							zap.Int("repeat_count", repeatCount),
						)
						if lastContent == "" {
							lastContent = fmt.Sprintf("抱歉，我在尝试执行操作时遇到了重复错误（连续 %d 次相同调用），已自动停止。请检查指令或重试。", repeatCount)
						}
						return lastContent, finishReason, nil
					}
				} else {
					lastToolSig = toolSig
					repeatCount = 1
				}

				if onProgress != nil {
					if content != "" {
						onProgress(ToolProgressEvent{
							Kind:    "thought",
							Content: content,
						})
					}
					// 发送每个工具的 tool_start 事件
					for _, tc := range msg.ToolCalls {
						callID := tc.ID
						if callID == "" {
							callID = tc.Function.Name
						}
						hint := formatToolHint([]schema.ToolCall{tc})
						onProgress(ToolProgressEvent{
							Kind:       "tool_start",
							Content:    hint,
							ToolCallID: callID,
							ToolName:   tc.Function.Name,
							ToolArgs:   tc.Function.Arguments,
						})
					}
				}
			}

			if content != "" {
				lastContent = content
			}

		case schema.Tool:
			// 捕获工具执行结果
			callID := msg.ToolCallID
			toolName := msg.ToolName
			// 也检查 MessageVariant.ToolName
			if toolName == "" {
				toolName = mv.ToolName
			}
			resultContent := msg.Content

			// 如果 callID 为空，尝试用 toolName 作为 fallback key
			lookupKey := callID
			if lookupKey == "" {
				lookupKey = toolName
			}

			// 如果 ToolName 为空，从追踪 map 中获取
			if toolName == "" {
				toolName = toolNames[lookupKey]
			}

			// 计算耗时
			var durationMs int64
			if startTime, found := toolStartTimes[lookupKey]; found {
				durationMs = time.Since(startTime).Milliseconds()
				delete(toolStartTimes, lookupKey)
			}

			// 生成结果预览（截取前 200 字符）
			preview := resultContent
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}

			// 判断成功/失败（简单启发式）
			success := true
			lowerResult := strings.ToLower(resultContent)
			if strings.HasPrefix(lowerResult, "error:") || strings.HasPrefix(lowerResult, "failed:") {
				success = false
			}

			l.logger.Info("🔧 Tool result",
				zap.String("trace_id", traceID),
				zap.String("tool", toolName),
				zap.String("call_id", callID),
				zap.Int64("duration_ms", durationMs),
				zap.Bool("success", success),
				zap.String("preview", preview),
			)

			if onProgress != nil {
				args := toolArgs[lookupKey]
				hint := toolName + "()"
				if args != "" {
					hint = formatToolHint([]schema.ToolCall{{
						ID:       callID,
						Function: schema.FunctionCall{Name: toolName, Arguments: args},
					}})
				}
				onProgress(ToolProgressEvent{
					Kind:          "tool_done",
					Content:       hint,
					ToolCallID:    callID,
					ToolName:      toolName,
					ToolArgs:      args,
					DurationMs:    durationMs,
					Success:       success,
					ResultPreview: preview,
				})
			}

			// 清理追踪
			delete(toolNames, lookupKey)
			delete(toolArgs, lookupKey)
		}
	}

	return lastContent, finishReason, nil
}

func traceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if traceID, ok := ctx.Value(agentTraceIDContextKey).(string); ok {
		return traceID
	}
	return ""
}

func withMessageTrace(ctx context.Context, traceID string) context.Context {
	if ctx == nil || traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, agentTraceIDContextKey, traceID)
}

func buildMessageTraceID(msg *bus.InboundMessage) string {
	if msg == nil {
		return ""
	}
	if messageID := extractMessageID(msg); messageID != "" {
		return fmt.Sprintf("%s:%s", msg.Channel, messageID)
	}
	ts := msg.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	return fmt.Sprintf("%s:%s:%s:%s:%d", msg.Channel, msg.AccountID, msg.ChatID, msg.SenderID, ts.UnixNano())
}

func extractMessageID(msg *bus.InboundMessage) string {
	if msg == nil || msg.Metadata == nil {
		return ""
	}
	if id, ok := msg.Metadata["message_id"]; ok {
		return fmt.Sprintf("%v", id)
	}
	return ""
}

func inboundLogFields(msg *bus.InboundMessage, traceID string) []zap.Field {
	if msg == nil {
		return []zap.Field{zap.String("trace_id", traceID)}
	}
	fields := []zap.Field{
		zap.String("trace_id", traceID),
		zap.String("channel", msg.Channel),
		zap.String("account_id", msg.AccountID),
		zap.String("chat_id", msg.ChatID),
		zap.String("sender_id", msg.SenderID),
		zap.String("session_key", msg.SessionKey()),
		zap.String("message_id", extractMessageID(msg)),
		zap.Int("content_length", len(msg.Content)),
		zap.String("content_preview", previewText(msg.Content, 160)),
		zap.Int("media_count", len(msg.Media)),
	}
	if !msg.Timestamp.IsZero() {
		fields = append(fields, zap.Time("timestamp", msg.Timestamp))
	}
	return fields
}

func outboundLogFields(msg *bus.OutboundMessage, traceID string) []zap.Field {
	if msg == nil {
		return []zap.Field{zap.String("trace_id", traceID)}
	}
	fields := []zap.Field{
		zap.String("trace_id", traceID),
		zap.String("channel", msg.Channel),
		zap.String("account_id", msg.AccountID),
		zap.String("chat_id", msg.ChatID),
		zap.String("reply_to", msg.ReplyTo),
		zap.Int("content_length", len(msg.Content)),
		zap.String("content_preview", previewText(msg.Content, 160)),
		zap.Int("media_count", len(msg.Media)),
	}
	if msg.Metadata != nil {
		if progress, ok := msg.Metadata["_progress"].(bool); ok {
			fields = append(fields, zap.Bool("is_progress", progress))
		}
		if toolHint, ok := msg.Metadata["_tool_hint"].(bool); ok {
			fields = append(fields, zap.Bool("is_tool_hint", toolHint))
		}
		if toolStatus, ok := msg.Metadata["_tool_status"].(string); ok {
			fields = append(fields, zap.String("tool_status", toolStatus))
		}
	}
	return fields
}

func previewText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" || limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func buildModelInputSnapshot(messages []*schema.Message) []map[string]any {
	snapshot := make([]map[string]any, 0, len(messages))
	for i, msg := range messages {
		if msg == nil {
			snapshot = append(snapshot, map[string]any{
				"index": i,
				"nil":   true,
			})
			continue
		}

		item := map[string]any{
			"index":                 i,
			"role":                  msg.Role,
			"content_length":        len(msg.Content),
			"content_preview":       previewText(msg.Content, 120),
			"multi_content_count":   len(msg.MultiContent),
			"tool_calls_count":      len(msg.ToolCalls),
			"tool_call_id":          msg.ToolCallID,
			"tool_name":             msg.ToolName,
			"reasoning_content_len": len(msg.ReasoningContent),
		}

		if len(msg.MultiContent) > 0 {
			partTypes := make([]string, 0, len(msg.MultiContent))
			for _, part := range msg.MultiContent {
				partTypes = append(partTypes, string(part.Type))
			}
			item["multi_content_types"] = partTypes
		}

		snapshot = append(snapshot, item)
	}
	return snapshot
}

// setToolContext 更新工具上下文
func (l *AgentLoop) setToolContext(channel, accountID, chatID string) {
	for _, name := range l.tools.Names() {
		t := l.tools.Get(name)
		if setter, ok := t.(tools.AccountContextSetter); ok {
			setter.SetContextWithAccount(channel, accountID, chatID)
			continue
		}
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
	// 确保 MCP 工具已连接（单消息模式不走 Run()，需要在此处连接）
	l.connectMCP(ctx)

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
