package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/yangkun19921001/PP-Claw/agent/tools"
	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/config"
	"github.com/yangkun19921001/PP-Claw/cron"
	"github.com/yangkun19921001/PP-Claw/rl"
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

// AgentLoop 多 Agent 并发 dispatcher。
// 通过 Router 将消息路由到目标 Agent，通过 Pool 获取 AgentEnv，
// 每条消息在独立 goroutine 中处理，per-session 串行保证。
type AgentLoop struct {
	bus         *bus.MessageBus
	cfg         *config.Config
	logger      *zap.Logger
	router      *AgentRouter
	pool        *AgentEnvPool
	cronService *cron.Service
	running     bool
}

// AgentLoopConfig 循环配置
type AgentLoopConfig struct {
	Bus         *bus.MessageBus
	Config      *config.Config
	Logger      *zap.Logger
	Router      *AgentRouter
	Pool        *AgentEnvPool
	CronService *cron.Service
}

// NewAgentLoop 创建 Agent 循环
func NewAgentLoop(cfg *AgentLoopConfig) *AgentLoop {
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AgentLoop{
		bus:         cfg.Bus,
		cfg:         cfg.Config,
		logger:      logger,
		router:      cfg.Router,
		pool:        cfg.Pool,
		cronService: cfg.CronService,
	}
}

// Run 运行 Agent 循环：消费入站消息，路由到目标 Agent，并发分发处理。
func (l *AgentLoop) Run(ctx context.Context) error {
	l.running = true
	l.logger.Info("Agent loop started (multi-agent dispatcher)")

	for l.running {
		msg, err := l.bus.ConsumeInbound(ctx)
		if err != nil {
			if err == context.Canceled {
				return nil
			}
			continue
		}

		agentID := l.router.ResolveWithContent(ctx, msg.Channel, msg.AccountID, msg.ChatID, msg.SenderID, msg.Content)
		go l.dispatch(ctx, agentID, msg)
	}

	return nil
}

// dispatch 分发单条消息到目标 AgentEnv，per-session 串行保证。
func (l *AgentLoop) dispatch(ctx context.Context, agentID string, msg *bus.InboundMessage) {
	traceID := buildMessageTraceID(msg)
	msgCtx := withMessageTrace(ctx, traceID)
	consumeStarted := time.Now()

	inboundFields := inboundLogFields(msg, traceID)
	inboundFields = append(inboundFields,
		zap.String("agent_id", agentID),
		zap.Int("inbound_queue_remaining", l.bus.InboundSize()),
	)
	l.logger.Info("消息链路: 收到入站消息", inboundFields...)

	env, err := l.pool.GetOrCreate(agentID)
	if err != nil {
		l.logger.Error("消息链路: 获取 AgentEnv 失败",
			append(inboundFields, zap.Error(err))...,
		)
		errMsg := bus.NewOutboundMessage(msg.Channel, msg.ChatID,
			fmt.Sprintf("Sorry, I encountered an error initializing agent: %s", err.Error()))
		errMsg.AccountID = msg.AccountID
		errMsg.ReplyTo = extractReplyTo(msg)
		l.bus.PublishOutbound(errMsg)
		return
	}

	// 连接 MCP（首次使用时懒连接）
	env.ConnectMCP(msgCtx, l.cfg)

	// /stop 命令需要在获取 session 锁之前处理，否则会被当前运行中的请求阻塞
	sessionKey := agentID + ":" + msg.SessionKey()
	cmd := strings.TrimSpace(strings.ToLower(msg.Content))
	if cmd == "/stop" {
		cancelled := 0
		if val, ok := env.ActiveTasks.Load(sessionKey); ok {
			if cancels, ok := val.([]context.CancelFunc); ok {
				for _, cancel := range cancels {
					cancel()
				}
				cancelled += len(cancels)
			}
			env.ActiveTasks.Delete(sessionKey)
		}
		if env.Subagents != nil {
			cancelled += env.Subagents.CancelBySession(sessionKey)
		}
		text := fmt.Sprintf("Cancelled %d task(s).", cancelled)
		if cancelled == 0 {
			text = "No active tasks to cancel."
		}
		resp := bus.NewOutboundMessage(msg.Channel, msg.ChatID, text)
		resp.AccountID = msg.AccountID
		l.bus.PublishOutbound(resp)
		return
	}

	// Per-session 串行锁
	mu := env.AcquireSession(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	// 创建可取消的子 context 并注册到 ActiveTasks，供 /stop 命令使用
	msgCtx, msgCancel := context.WithCancel(msgCtx)
	defer msgCancel()

	// 注册 cancel func
	if val, ok := env.ActiveTasks.Load(sessionKey); ok {
		env.ActiveTasks.Store(sessionKey, append(val.([]context.CancelFunc), msgCancel))
	} else {
		env.ActiveTasks.Store(sessionKey, []context.CancelFunc{msgCancel})
	}
	defer func() {
		// 任务完成后清理（简单起见直接删除，因为 per-session 串行锁保证同一时间只有一个任务）
		env.ActiveTasks.Delete(sessionKey)
	}()

	response, err := processMessage(msgCtx, env, msg, sessionKey, l.bus, l.logger)
	if err != nil {
		l.logger.Error("消息链路: 处理消息失败",
			append(inboundFields,
				zap.Int64("duration_ms", time.Since(consumeStarted).Milliseconds()),
				zap.Error(err),
			)...,
		)
		errMsg := bus.NewOutboundMessage(msg.Channel, msg.ChatID,
			fmt.Sprintf("Sorry, I encountered an error: %s", err.Error()))
		errMsg.AccountID = msg.AccountID
		errMsg.ReplyTo = extractReplyTo(msg)
		l.bus.PublishOutbound(errMsg)
		return
	}
	if response != nil {
		l.logger.Info("消息链路: 发布最终响应",
			append(
				outboundLogFields(response, traceID),
				zap.Int64("duration_ms", time.Since(consumeStarted).Milliseconds()),
			)...,
		)
		l.bus.PublishOutbound(response)
	}
}

// Stop 停止循环
func (l *AgentLoop) Stop() {
	l.running = false
	l.pool.Close()
	l.logger.Info("Agent loop stopping")
}

// GetFeishuOAuthHandler 返回飞书 OAuth 回调的 HTTP handler
func (l *AgentLoop) GetFeishuOAuthHandler() http.Handler {
	if h := l.pool.GetFeishuOAuthHandler(); h != nil {
		if handler, ok := h.(http.Handler); ok {
			return handler
		}
	}
	return nil
}

// ProcessDirect 直接处理消息 (用于 CLI 单消息模式)
func (l *AgentLoop) ProcessDirect(ctx context.Context, content string) (string, error) {
	defaultID := l.cfg.ResolveDefaultAgentID()
	env, err := l.pool.GetOrCreate(defaultID)
	if err != nil {
		return "", err
	}
	env.ConnectMCP(ctx, l.cfg)

	msg := bus.NewInboundMessage("cli", "user", "direct", content)
	sessionKey := defaultID + ":" + msg.SessionKey()
	resp, err := processMessage(ctx, env, msg, sessionKey, l.bus, l.logger)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return resp.Content, nil
}

// ============ 包级处理函数（使用 AgentEnv） ============

// processMessage 处理单条消息
func processMessage(ctx context.Context, env *AgentEnv, msg *bus.InboundMessage, sessionKey string, msgBus *bus.MessageBus, logger *zap.Logger) (response *bus.OutboundMessage, err error) {
	traceID := traceIDFromContext(ctx)
	if traceID == "" {
		traceID = buildMessageTraceID(msg)
		ctx = withMessageTrace(ctx, traceID)
	}

	started := time.Now()
	result := "processing"
	defer func() {
		fields := append(inboundLogFields(msg, traceID),
			zap.String("agent_id", env.AgentID),
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
		logger.Info("消息链路: processMessage 结束", fields...)
	}()

	logger.Info("消息链路: 进入处理流水线",
		append(inboundLogFields(msg, traceID), zap.String("agent_id", env.AgentID))...,
	)

	// 更新工具上下文
	env.SetToolContext(msg.Channel, msg.AccountID, msg.ChatID)

	// 开始新的对话回复回合
	replyTo := extractReplyTo(msg)
	if t := env.Tools.Get("message"); t != nil {
		if mt, ok := t.(*tools.MessageTool); ok {
			mt.StartTurn()
			mt.SetReplyTo(replyTo)
		}
	}
	if t := env.Tools.Get("cron"); t != nil {
		if ct, ok := t.(*tools.CronTool); ok {
			ct.StartTurn()
		}
	}

	// Handle system messages
	if msg.Channel == "system" {
		return handleSystemMessage(ctx, env, msg, sessionKey, msgBus, logger)
	}

	// 处理斜杠命令
	cmd := strings.TrimSpace(strings.ToLower(msg.Content))
	if cmd == "/stop" {
		return handleStop(env, msg, sessionKey)
	}
	if cmd == "/new" {
		sess := env.Sessions.GetOrCreate(sessionKey)
		if len(sess.Messages) > 0 {
			consolidateMemory(env, ctx, sess, true, logger)
		}
		sess.Clear()
		env.Sessions.Save(sess)
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
	sess := env.Sessions.GetOrCreate(sessionKey)

	// 开始自学习会话跟踪
	if env.Learning != nil {
		_ = env.Learning.BeginConversationTracking(ctx, sessionKey, env.AgentID)
	}

	// 构建消息上下文
	history := sess.GetHistory(env.MemoryWindow)
	logger.Info("消息链路: 会话上下文准备完成",
		zap.String("trace_id", traceID),
		zap.String("session_key", sessionKey),
		zap.Int("history_count", len(history)),
		zap.Int("media_count", len(msg.Media)),
	)

	// 构建 Eino 消息: system prompt + history + user message
	var einoMsgs []*schema.Message

	sysPrompt := env.Context.BuildSystemPrompt()
	// 注入已学习的技能到系统提示词
	if env.Learning != nil {
		sysPrompt, _ = env.Learning.EnhanceSystemPrompt(ctx, env.AgentID, sysPrompt, msg.Content)
	}
	einoMsgs = append(einoMsgs, &schema.Message{
		Role:    schema.System,
		Content: sysPrompt,
	})

	// Skip leading assistant messages in history to ensure the first
	// non-system message is always a user message (LLM requirement).
	startIdx := 0
	for startIdx < len(history) {
		role, _ := history[startIdx]["role"].(string)
		if role == "user" {
			break
		}
		startIdx++
	}
	for _, h := range history[startIdx:] {
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

	// Current user message with runtime context (supports multimodal)
	isCronMessage := msg.SenderID == "cron"
	userContent := env.Context.buildUserContent(msg.Content, msg.Media)
	userContent = env.Context.injectRuntimeContext(userContent, msg.Channel, msg.ChatID)

	cronSuffix := ""
	if isCronMessage {
		cronSuffix = "\n\n[Scheduled Task — Tool Call Required]\n" +
			"This is an automated scheduled task. You MUST call tools (web_search, web_fetch, execute, read_file, etc.) to complete it.\n" +
			"Do NOT just describe or acknowledge what you would do — actually invoke the tools NOW to produce real results.\n" +
			"If you respond without calling any tool, the task will be considered failed."
	}

	userMsg := &schema.Message{Role: schema.User}
	switch v := userContent.(type) {
	case string:
		userMsg.Content = v + cronSuffix
	case []map[string]any:
		userMsg.UserInputMultiContent = buildEinoMultiContent(v, cronSuffix)
	default:
		userMsg.Content = msg.Content + cronSuffix
	}
	einoMsgs = append(einoMsgs, userMsg)

	logger.Info("消息链路: 模型输入构建完成",
		zap.String("trace_id", traceID),
		zap.String("session_key", sessionKey),
		zap.Int("eino_messages", len(einoMsgs)),
		zap.Int("history_count", len(history)),
		zap.Int("attached_media_count", len(msg.Media)),
		zap.Bool("multimodal", userMsg.UserInputMultiContent != nil),
		zap.Int("user_content_length", len(userMsg.Content)),
		zap.Any("message_snapshot", buildModelInputSnapshot(einoMsgs)),
	)

	// 构建 progress 回调
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
		logger.Info("消息链路: 发布进度消息", progressFields...)
		msgBus.PublishOutbound(progressMsg)
	}

	// 通过 ADK Runner 执行
	logger.Info("消息链路: 开始执行 Agent",
		zap.String("trace_id", traceID),
		zap.String("session_key", sessionKey),
	)
	finalContent, finishReason, toolCallCount, err := runWithADK(ctx, env, sessionKey, einoMsgs, onProgress, logger)
	if err != nil {
		logger.Error("消息链路: 模型调用失败，输出输入快照用于排查",
			zap.String("trace_id", traceID),
			zap.String("session_key", sessionKey),
			zap.Int("eino_messages", len(einoMsgs)),
			zap.Any("message_snapshot", buildModelInputSnapshot(einoMsgs)),
			zap.Error(err),
		)
		result = "agent_error"
		return nil, err
	}
	logger.Info("消息链路: Agent 执行完成",
		zap.String("trace_id", traceID),
		zap.String("session_key", sessionKey),
		zap.Int("final_content_length", len(finalContent)),
		zap.String("finish_reason", finishReason),
		zap.Int("tool_call_count", toolCallCount),
	)

	// Fix 3: cron 触发的消息如果首次未调用任何工具，自动重试一次
	if isCronMessage && toolCallCount == 0 && finishReason == "ok" {
		logger.Warn("消息链路: cron 任务首次执行未调用工具，自动重试",
			zap.String("trace_id", traceID),
			zap.String("session_key", sessionKey),
			zap.Int("first_content_length", len(finalContent)),
		)
		// 追加 assistant 的空回复和系统纠正提示再重试
		retryMsgs := make([]*schema.Message, len(einoMsgs))
		copy(retryMsgs, einoMsgs)
		retryMsgs = append(retryMsgs,
			&schema.Message{Role: schema.Assistant, Content: finalContent},
			&schema.Message{Role: schema.User, Content: "你上一次回复没有调用任何工具。这是一个自动化定时任务，必须实际执行工具调用（如 web_search、web_fetch、execute 等）才能完成。请立即调用工具执行任务，不要再仅输出文本描述。"},
		)
		finalContent, finishReason, toolCallCount, err = runWithADK(ctx, env, sessionKey, retryMsgs, onProgress, logger)
		if err != nil {
			logger.Error("消息链路: cron 重试执行失败",
				zap.String("trace_id", traceID),
				zap.Error(err),
			)
			result = "agent_error"
			return nil, err
		}
		logger.Info("消息链路: cron 重试执行完成",
			zap.String("trace_id", traceID),
			zap.Int("retry_content_length", len(finalContent)),
			zap.Int("retry_tool_call_count", toolCallCount),
		)
	}

	if finalContent == "" {
		finalContent = "I've completed processing but have no response to give."
	}
	if shouldBlockHallucinatedCronSuccess(msg.Content, finalContent, env) {
		logger.Warn("消息链路: 拦截未实际创建的定时任务成功回复",
			zap.String("trace_id", traceID),
			zap.String("channel", msg.Channel),
			zap.String("chat_id", msg.ChatID),
			zap.String("sender_id", msg.SenderID),
			zap.String("user_content", msg.Content),
			zap.String("assistant_content", finalContent),
		)
		finalContent = "这次没有实际创建定时任务：本轮没有调用 cron 工具，所以任务不会入库，也不会触发。请重试，我会直接用 cron 工具创建。"
		finishReason = "cron_tool_missing"
	}

	responsePreview := finalContent
	if len(responsePreview) > 200 {
		responsePreview = responsePreview[:200] + "..."
	}
	logger.Info("🤖 Agent response",
		zap.String("trace_id", traceID),
		zap.String("channel", msg.Channel),
		zap.String("chat_id", msg.ChatID),
		zap.Int("length", len(finalContent)),
		zap.String("preview", responsePreview),
	)

	// 保存会话
	// Fix 4: cron 触发且未调用工具的短响应不写入 session，防止历史投毒
	if finishReason == "error" {
		logger.Warn("消息链路: 错误响应不保存到会话",
			zap.String("trace_id", traceID),
			zap.String("session_key", sessionKey),
		)
	} else if isCronMessage && toolCallCount == 0 && len(finalContent) < 500 {
		logger.Warn("消息链路: cron 无效响应（无工具调用）不保存到会话",
			zap.String("trace_id", traceID),
			zap.String("session_key", sessionKey),
			zap.Int("final_content_length", len(finalContent)),
		)
	} else {
		saveTurn(env, sess, msg.Content, finalContent, msg.Media)
	}

	// 完成自学习会话跟踪（异步触发学习）
	if env.Learning != nil {
		_ = env.Learning.FinishConversationTracking(ctx, sessionKey, env.AgentID)
	}

	// 检查是否需要触发记忆合并
	unconsolidated := len(sess.Messages) - sess.LastConsolidated
	if unconsolidated >= env.MemoryWindow {
		logger.Info("消息链路: 触发异步记忆合并",
			zap.String("trace_id", traceID),
			zap.String("session_key", sessionKey),
			zap.Int("unconsolidated", unconsolidated),
			zap.Int("memory_window", env.MemoryWindow),
		)
		go consolidateMemory(env, context.Background(), sess, false, logger)
	}

	// 如果 MessageTool 已经在本轮回合中发送过消息，则不再重复发送
	if t := env.Tools.Get("message"); t != nil {
		if mt, ok := t.(*tools.MessageTool); ok && mt.SentInTurn {
			result = "message_tool_sent"
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

func shouldBlockHallucinatedCronSuccess(userContent, assistantContent string, env *AgentEnv) bool {
	if !looksLikeCronRequest(userContent) || !looksLikeCronSuccessReply(assistantContent) {
		return false
	}
	t := env.Tools.Get("cron")
	if t == nil {
		return true
	}
	ct, ok := t.(*tools.CronTool)
	if !ok {
		return true
	}
	return !ct.UsedInTurn
}

func looksLikeCronRequest(content string) bool {
	content = strings.ToLower(strings.TrimSpace(content))
	if content == "" {
		return false
	}
	keywords := []string{
		"定时", "定时器", "定时任务", "提醒我", "提醒", "分钟后", "小时后", "后推送", "后提醒",
		"schedule", "scheduled", "remind me", "reminder", "timer", "cron",
	}
	for _, kw := range keywords {
		if strings.Contains(content, kw) {
			return true
		}
	}
	timeLike := regexp.MustCompile(`\b\d{1,2}:\d{2}\b|[0-9一二三四五六七八九十两]+分钟后|[0-9一二三四五六七八九十两]+小时后`)
	return timeLike.MatchString(content) &&
		(strings.Contains(content, "任务") || strings.Contains(content, "提醒") || strings.Contains(content, "推送"))
}

func looksLikeCronSuccessReply(content string) bool {
	content = strings.ToLower(strings.TrimSpace(content))
	if content == "" {
		return false
	}
	phrases := []string{
		"已成功设置", "任务已成功设置", "定时任务已成功设置", "定时器测试任务已成功设置",
		"提醒已设置", "已为你设置", "created job", "scheduled", "任务已重新设置",
		"任务已创建", "定时任务已创建", "提醒已创建", "创建成功", "已创建任务",
	}
	for _, phrase := range phrases {
		if strings.Contains(content, phrase) {
			return true
		}
	}
	return false
}

// handleStop cancels all active tasks for the session.
func handleStop(env *AgentEnv, msg *bus.InboundMessage, sessionKey string) (*bus.OutboundMessage, error) {
	cancelled := 0

	if val, ok := env.ActiveTasks.Load(sessionKey); ok {
		if cancels, ok := val.([]context.CancelFunc); ok {
			for _, cancel := range cancels {
				cancel()
			}
			cancelled += len(cancels)
		}
		env.ActiveTasks.Delete(sessionKey)
	}

	if env.Subagents != nil {
		cancelled += env.Subagents.CancelBySession(sessionKey)
	}

	text := fmt.Sprintf("Cancelled %d task(s).", cancelled)
	if cancelled == 0 {
		text = "No active tasks to cancel."
	}

	out := bus.NewOutboundMessage(msg.Channel, msg.ChatID, text)
	out.AccountID = msg.AccountID
	return out, nil
}

// handleSystemMessage processes system channel messages (subagent results, etc.)
func handleSystemMessage(ctx context.Context, env *AgentEnv, msg *bus.InboundMessage, sessionKey string, msgBus *bus.MessageBus, logger *zap.Logger) (*bus.OutboundMessage, error) {
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
			logger.Warn("系统消息格式错误", zap.String("chat_id", msg.ChatID))
			return nil, nil
		}
		targetChannel = parts[0]
		targetChatID = parts[1]
	}

	role := "user"
	if msg.SenderID == "subagent" {
		role = "assistant"
	}

	// 使用传入的 sessionKey（已含 agentID 前缀）或按目标重构
	sysSessionKey := sessionKey
	if targetChannel != msg.Channel || targetChatID != msg.ChatID {
		sysSessionKey = env.AgentID + ":" + targetChannel + ":"
		if targetAccountID != "" {
			sysSessionKey += targetAccountID + ":"
		}
		sysSessionKey += targetChatID
	}
	sess := env.Sessions.GetOrCreate(sysSessionKey)

	sess.AddMessage(role, msg.Content)
	env.Sessions.Save(sess)

	history := sess.GetHistory(env.MemoryWindow)
	var einoMsgs []*schema.Message
	einoMsgs = append(einoMsgs, &schema.Message{
		Role:    schema.System,
		Content: env.Context.BuildSystemPrompt(),
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

	if len(einoMsgs) > 0 && einoMsgs[len(einoMsgs)-1].Role != schema.User {
		einoMsgs = append(einoMsgs, &schema.Message{
			Role:    schema.User,
			Content: "Please summarize the above result for the user.",
		})
	}

	env.SetToolContext(targetChannel, targetAccountID, targetChatID)

	finalContent, _, _, err := runWithADK(ctx, env, "", einoMsgs, nil, logger)
	if err != nil {
		logger.Error("系统消息处理失败", zap.Error(err))
		return nil, nil
	}

	if finalContent == "" {
		return nil, nil
	}

	sess.AddMessage("assistant", finalContent)
	env.Sessions.Save(sess)

	out := bus.NewOutboundMessage(targetChannel, targetChatID, finalContent)
	out.AccountID = targetAccountID
	msgBus.PublishOutbound(out)
	return nil, nil
}

// runtimeContextRE matches <runtime_context>...</runtime_context> tags
var runtimeContextRE = regexp.MustCompile(`(?s)<runtime_context>.*?</runtime_context>\s*`)

// base64ImageRE matches base64-encoded image data URIs
var base64ImageRE = regexp.MustCompile(`data:image/[^;]+;base64,[A-Za-z0-9+/=]{100,}`)

// saveTurn saves user+assistant messages to the session with cleanup
func saveTurn(env *AgentEnv, sess *session.Session, userContent, assistantContent string, userMedia []string) {
	cleanUser := runtimeContextRE.ReplaceAllString(userContent, "")
	cleanUser = strings.TrimSpace(cleanUser)
	cleanUser = base64ImageRE.ReplaceAllString(cleanUser, "[image]")

	if cleanUser != "" || len(userMedia) > 0 {
		sess.AddMessageWithMedia("user", cleanUser, userMedia)
	}

	cleanAssistant := base64ImageRE.ReplaceAllString(assistantContent, "[image]")
	if len(cleanAssistant) > 16000 {
		cleanAssistant = cleanAssistant[:16000] + "\n[truncated]"
	}

	if cleanAssistant != "" {
		sess.AddMessage("assistant", cleanAssistant)
	}

	env.Sessions.Save(sess)
}

// consolidateMemory 执行记忆合并（带锁防止并发合并同一 session）
func consolidateMemory(env *AgentEnv, ctx context.Context, sess *session.Session, archiveAll bool, logger *zap.Logger) {
	env.ConsolidateMu.Lock()
	defer env.ConsolidateMu.Unlock()

	newOffset, ok := env.Memory.Consolidate(ctx, sess.Messages, archiveAll, env.MemoryWindow, sess.LastConsolidated)
	if ok {
		sess.LastConsolidated = newOffset
		env.Sessions.Save(sess)
		logger.Info("记忆合并完成", zap.String("session", sess.Key), zap.Int("new_offset", newOffset))
	}
}

// runWithADK 通过 Eino ADK Runner 运行。返回 (最终内容, 结束原因, 工具调用总数, error)。
func runWithADK(ctx context.Context, env *AgentEnv, sessionKey string, messages []*schema.Message, onProgress func(ToolProgressEvent), logger *zap.Logger) (string, string, int, error) {
	iter := env.ADKRunner.Run(ctx, messages)
	traceID := traceIDFromContext(ctx)

	logger.Info("ADK request snapshot",
		zap.String("trace_id", traceID),
		zap.Any("messages", buildFullModelInputSnapshot(messages)),
	)

	var lastContent string
	finishReason := "ok"
	totalToolCalls := 0

	const maxRepeatErrors = 3
	lastToolSig := ""
	repeatCount := 0

	toolStartTimes := make(map[string]time.Time)
	toolNames := make(map[string]string)
	toolArgs := make(map[string]string)
	eventCount := 0

	for {
		event, ok := iter.Next()
		if !ok {
			logger.Info("ADK 迭代结束",
				zap.String("trace_id", traceID),
				zap.Int("total_events", eventCount),
				zap.Int("last_content_length", len(lastContent)),
			)
			break
		}
		eventCount++
		if event.Err != nil {
			return "", "error", totalToolCalls, fmt.Errorf("agent error: %w", event.Err)
		}

		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		mv := event.Output.MessageOutput
		role := mv.Role

		msg, msgErr := mv.GetMessage()
		if msgErr != nil {
			logger.Warn("GetMessage error",
				zap.String("trace_id", traceID),
				zap.Error(msgErr),
				zap.String("role", string(role)),
			)
			continue
		}
		if msg == nil {
			continue
		}

		contentPreview := msg.Content
		if len(contentPreview) > 200 {
			contentPreview = contentPreview[:200] + "...(truncated)"
		}
		logger.Info("ADK event",
			zap.String("trace_id", traceID),
			zap.String("mv_role", string(role)),
			zap.String("msg_role", string(msg.Role)),
			zap.String("tool_call_id", msg.ToolCallID),
			zap.String("tool_name", msg.ToolName),
			zap.Int("tool_calls", len(msg.ToolCalls)),
			zap.Int("content_length", len(msg.Content)),
			zap.String("content_preview", contentPreview),
		)
		logger.Info("ADK raw event",
			zap.String("trace_id", traceID),
			zap.Any("event", buildADKEventSnapshot(event, mv, msg)),
		)

		switch role {
		case schema.Assistant:
			content := stripThink(msg.Content)

			if len(msg.ToolCalls) == 0 && content != "" && onProgress != nil {
				onProgress(ToolProgressEvent{
					Kind:    "thought",
					Content: content,
				})
			}

			if len(msg.ToolCalls) > 0 {
				totalToolCalls += len(msg.ToolCalls)
				toolSig := ""
				for _, tc := range msg.ToolCalls {
					toolSig += tc.Function.Name + ":" + tc.Function.Arguments + "|"
					logger.Info("🤖 Assistant tool call",
						zap.String("trace_id", traceID),
						zap.String("tool", tc.Function.Name),
						zap.String("args", tc.Function.Arguments),
						zap.String("call_id", tc.ID),
					)
					key := tc.ID
					if key == "" {
						key = tc.Function.Name
					}
					toolStartTimes[key] = time.Now()
					toolNames[key] = tc.Function.Name
					toolArgs[key] = tc.Function.Arguments
				}
				if toolSig == lastToolSig {
					repeatCount++
					if repeatCount >= maxRepeatErrors {
						logger.Warn("🔄 检测到工具调用死循环，强制终止",
							zap.String("trace_id", traceID),
							zap.String("tool_sig", toolSig),
							zap.Int("repeat_count", repeatCount),
						)
						if lastContent == "" {
							lastContent = fmt.Sprintf("抱歉，我在尝试执行操作时遇到了重复错误（连续 %d 次相同调用），已自动停止。请检查指令或重试。", repeatCount)
						}
						return lastContent, finishReason, totalToolCalls, nil
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
			callID := msg.ToolCallID
			toolName := msg.ToolName
			if toolName == "" {
				toolName = mv.ToolName
			}
			resultContent := msg.Content

			lookupKey := callID
			if lookupKey == "" {
				lookupKey = toolName
			}
			if toolName == "" {
				toolName = toolNames[lookupKey]
			}

			var durationMs int64
			if startTime, found := toolStartTimes[lookupKey]; found {
				durationMs = time.Since(startTime).Milliseconds()
				delete(toolStartTimes, lookupKey)
			}

			preview := resultContent
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}

			success := true
			lowerResult := strings.ToLower(resultContent)
			if strings.HasPrefix(lowerResult, "error:") || strings.HasPrefix(lowerResult, "failed:") {
				success = false
			}

			logger.Info("🔧 Tool result",
				zap.String("trace_id", traceID),
				zap.String("tool", toolName),
				zap.String("call_id", callID),
				zap.Int64("duration_ms", durationMs),
				zap.Bool("success", success),
				zap.String("preview", preview),
			)

			// 记录工具执行步骤到自学习轨迹
			if env.Learning != nil && sessionKey != "" {
				var actionInput map[string]any
				if raw := toolArgs[lookupKey]; raw != "" {
					_ = json.Unmarshal([]byte(raw), &actionInput)
				}
				_ = env.Learning.RecordExecutionStep(ctx, sessionKey, &rl.TrajectoryStep{
					StepID:      callID,
					Timestamp:   time.Now(),
					Action:      toolName,
					ActionInput: actionInput,
					Observation: resultContent,
					Success:     success,
					Duration:    time.Duration(durationMs) * time.Millisecond,
				})
			}

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

			delete(toolNames, lookupKey)
			delete(toolArgs, lookupKey)
		}
	}

	return lastContent, finishReason, totalToolCalls, nil
}

// ============ 工具函数 ============

// formatToolHint 格式化工具调用提示
func formatToolHint(toolCalls []schema.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}
	tc := toolCalls[0]
	name := tc.Function.Name
	args := tc.Function.Arguments

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

func buildFullModelInputSnapshot(messages []*schema.Message) []map[string]any {
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
			"index":             i,
			"role":              msg.Role,
			"content":           msg.Content,
			"content_length":    len(msg.Content),
			"tool_call_id":      msg.ToolCallID,
			"tool_name":         msg.ToolName,
			"reasoning_content": msg.ReasoningContent,
		}
		if len(msg.ToolCalls) > 0 {
			item["tool_calls"] = buildToolCallSnapshot(msg.ToolCalls)
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

func buildADKEventSnapshot(event any, mv any, msg *schema.Message) map[string]any {
	snapshot := map[string]any{
		"event_type": fmt.Sprintf("%T", event),
		"variant":    fmt.Sprintf("%T", mv),
	}
	if msg == nil {
		snapshot["message_nil"] = true
		return snapshot
	}

	snapshot["message"] = map[string]any{
		"role":              msg.Role,
		"content":           msg.Content,
		"content_length":    len(msg.Content),
		"tool_call_id":      msg.ToolCallID,
		"tool_name":         msg.ToolName,
		"reasoning_content": msg.ReasoningContent,
		"tool_calls":        buildToolCallSnapshot(msg.ToolCalls),
	}
	return snapshot
}

func buildToolCallSnapshot(toolCalls []schema.ToolCall) []map[string]any {
	if len(toolCalls) == 0 {
		return nil
	}
	snapshot := make([]map[string]any, 0, len(toolCalls))
	for _, tc := range toolCalls {
		snapshot = append(snapshot, map[string]any{
			"id":        tc.ID,
			"type":      tc.Type,
			"index":     tc.Index,
			"function":  tc.Function.Name,
			"arguments": tc.Function.Arguments,
		})
	}
	return snapshot
}

// extractReplyTo 从入站消息的 Metadata 中提取 message_id 作为回复目标
func extractReplyTo(msg *bus.InboundMessage) string {
	if msg == nil || msg.Metadata == nil {
		return ""
	}
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
