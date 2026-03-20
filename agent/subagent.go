package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/yangkun19921001/PP-Claw/agent/tools"
	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/utils"
	"go.uber.org/zap"
)

// SubagentManager 后台子代理管理器 (对标 agent/subagent.py:SubagentManager)
type SubagentManager struct {
	workspace           string
	bus                 *bus.MessageBus
	model               string
	maxIterations       int
	braveAPIKey         string
	restrictToWorkspace bool
	logger              *zap.Logger
	chatModel           einomodel.ToolCallingChatModel
	skillsSummary       string // pre-built skills summary

	mu           sync.Mutex
	runningTasks map[string]context.CancelFunc
	// sessionTasks tracks cancels by session key for batch cancellation
	sessionTasks map[string]map[string]context.CancelFunc
}

// NewSubagentManager 创建子代理管理器
func NewSubagentManager(workspace string, msgBus *bus.MessageBus, model string, chatModel einomodel.ToolCallingChatModel, logger *zap.Logger) *SubagentManager {
	if logger == nil {
		logger = zap.NewNop()
	}

	// Build skills summary once
	skillsSummary := ""
	loader := NewSkillsLoader(workspace)
	if loader != nil {
		skillsSummary = loader.BuildSkillsSummary()
	}

	return &SubagentManager{
		workspace:     workspace,
		bus:           msgBus,
		model:         model,
		maxIterations: 15,
		logger:        logger,
		chatModel:     chatModel,
		skillsSummary: skillsSummary,
		runningTasks:  make(map[string]context.CancelFunc),
		sessionTasks:  make(map[string]map[string]context.CancelFunc),
	}
}

// Spawn 生成后台子代理 (对标 subagent.py:spawn)
func (m *SubagentManager) Spawn(ctx context.Context, task string, label string, originChannel, originChatID string) string {
	taskID := uuid.New().String()[:8]
	displayLabel := label
	if displayLabel == "" {
		displayLabel = task
		if len(displayLabel) > 30 {
			displayLabel = displayLabel[:30] + "..."
		}
	}

	sessionKey := originChannel + ":" + originChatID
	ctx2, cancel := context.WithCancel(ctx)

	m.mu.Lock()
	m.runningTasks[taskID] = cancel
	if m.sessionTasks[sessionKey] == nil {
		m.sessionTasks[sessionKey] = make(map[string]context.CancelFunc)
	}
	m.sessionTasks[sessionKey][taskID] = cancel
	m.mu.Unlock()

	go m.runSubagent(ctx2, taskID, task, displayLabel, originChannel, originChatID, sessionKey)

	m.logger.Info("子代理已生成", zap.String("id", taskID), zap.String("label", displayLabel))
	return fmt.Sprintf("Subagent [%s] started (id: %s). I'll notify you when it completes.", displayLabel, taskID)
}

// runSubagent 执行子代理任务 — 真实 LLM 循环 (对标 subagent.py:_run_subagent)
func (m *SubagentManager) runSubagent(ctx context.Context, taskID, task, label, originChannel, originChatID, sessionKey string) {
	defer func() {
		m.mu.Lock()
		delete(m.runningTasks, taskID)
		if tasks, ok := m.sessionTasks[sessionKey]; ok {
			delete(tasks, taskID)
			if len(tasks) == 0 {
				delete(m.sessionTasks, sessionKey)
			}
		}
		m.mu.Unlock()
	}()

	m.logger.Info("子代理启动", zap.String("id", taskID), zap.String("label", label))

	// Build subagent tools (no message, no spawn)
	registry := tools.NewRegistry()
	allowedDir := ""
	if m.restrictToWorkspace {
		allowedDir = m.workspace
	}
	registry.Register(&tools.ReadFileTool{Workspace: m.workspace, AllowedDir: allowedDir})
	registry.Register(&tools.WriteFileTool{Workspace: m.workspace, AllowedDir: allowedDir})
	registry.Register(&tools.EditFileTool{Workspace: m.workspace, AllowedDir: allowedDir})
	registry.Register(&tools.ListDirTool{Workspace: m.workspace, AllowedDir: allowedDir})
	registry.Register(&tools.ExecTool{WorkingDir: m.workspace})
	registry.Register(&tools.WebSearchTool{APIKey: m.braveAPIKey})
	registry.Register(&tools.WebFetchTool{MaxChars: 50000})

	// If no chatModel, fall back to stub
	if m.chatModel == nil {
		m.logger.Warn("子代理: chatModel 未设置，使用 stub 模式")
		finalResult := fmt.Sprintf("Subagent [%s] completed task: %s", label, task)
		m.announceResult(taskID, label, task, finalResult, originChannel, originChatID, "ok")
		return
	}

	// Bind tools to model: extract ToolInfo from each BaseTool
	einoTools := registry.ToEinoTools()
	toolInfos := make([]*schema.ToolInfo, 0, len(einoTools))
	for _, t := range einoTools {
		info, infoErr := t.Info(ctx)
		if infoErr != nil {
			m.logger.Warn("子代理: 获取工具信息失败", zap.Error(infoErr))
			continue
		}
		toolInfos = append(toolInfos, info)
	}

	boundModel, err := m.chatModel.WithTools(toolInfos)
	if err != nil {
		m.logger.Error("子代理: 绑定工具失败", zap.String("id", taskID), zap.Error(err))
		m.announceResult(taskID, label, task, "Failed to initialize: "+err.Error(), originChannel, originChatID, "error")
		return
	}

	// Build messages
	sysPrompt := m.buildSubagentPrompt()
	messages := []*schema.Message{
		{Role: schema.System, Content: sysPrompt},
		{Role: schema.User, Content: task},
	}

	// LLM loop
	var lastContent string
	for i := 0; i < m.maxIterations; i++ {
		select {
		case <-ctx.Done():
			m.logger.Info("子代理被取消", zap.String("id", taskID))
			m.announceResult(taskID, label, task, "Cancelled", originChannel, originChatID, "cancelled")
			return
		default:
		}

		resp, err := boundModel.Generate(ctx, messages)
		if err != nil {
			m.logger.Error("子代理: LLM 调用失败", zap.String("id", taskID), zap.Error(err))
			status := "error"
			result := "LLM error: " + err.Error()
			if lastContent != "" {
				result = lastContent
				status = "partial"
			}
			m.announceResult(taskID, label, task, result, originChannel, originChatID, status)
			return
		}

		content := stripThink(resp.Content)
		if content != "" {
			lastContent = content
		}

		// No tool calls — we're done
		if len(resp.ToolCalls) == 0 {
			break
		}

		// Append assistant message with tool calls
		messages = append(messages, resp)

		// Execute each tool call
		for _, tc := range resp.ToolCalls {
			toolResult := m.executeToolCall(ctx, registry, tc)
			messages = append(messages, &schema.Message{
				Role:       schema.Tool,
				Content:    toolResult,
				ToolCallID: tc.ID,
			})
		}
	}

	if lastContent == "" {
		lastContent = fmt.Sprintf("Subagent [%s] completed task but produced no output.", label)
	}

	m.announceResult(taskID, label, task, lastContent, originChannel, originChatID, "ok")
}

// executeToolCall executes a single tool call and returns the result string.
func (m *SubagentManager) executeToolCall(ctx context.Context, registry *tools.Registry, tc schema.ToolCall) string {
	tool := registry.Get(tc.Function.Name)
	if tool == nil {
		return fmt.Sprintf("Error: unknown tool %q", tc.Function.Name)
	}

	var params map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
		return fmt.Sprintf("Error: invalid arguments for %s: %v", tc.Function.Name, err)
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		return fmt.Sprintf("Error: %s failed: %v", tc.Function.Name, err)
	}

	// Truncate very long results
	if len(result) > 16000 {
		result = result[:16000] + "\n[truncated]"
	}

	return result
}

// CancelBySession cancels all running subagents for a session key.
// Returns the number of tasks cancelled.
func (m *SubagentManager) CancelBySession(sessionKey string) int {
	m.mu.Lock()
	tasks, ok := m.sessionTasks[sessionKey]
	if !ok {
		m.mu.Unlock()
		return 0
	}
	// Copy cancel funcs to avoid holding lock during cancellation
	cancels := make([]context.CancelFunc, 0, len(tasks))
	for _, cancel := range tasks {
		cancels = append(cancels, cancel)
	}
	m.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels)
}

// announceResult 回报子代理结果 (对标 subagent.py:_announce_result)
func (m *SubagentManager) announceResult(taskID, label, task, result, originChannel, originChatID, status string) {
	// Evaluate if the result warrants notification
	if m.chatModel != nil && status == "ok" {
		ctx := context.Background()
		taskContext := fmt.Sprintf("Subagent '%s' task: %s", label, task)
		if !utils.EvaluateResponse(ctx, result, taskContext, m.chatModel, m.logger) {
			m.logger.Info("子代理结果评估: 不需要通知",
				zap.String("id", taskID),
				zap.String("label", label),
			)
			return
		}
	}
	statusText := "completed successfully"
	if status != "ok" {
		statusText = "failed"
	}

	content := fmt.Sprintf(`[Subagent '%s' %s]

Task: %s

Result:
%s

Summarize this naturally for the user. Keep it brief (1-2 sentences). Do not mention technical details like "subagent" or task IDs.`,
		label, statusText, task, result)

	msg := &bus.InboundMessage{
		Channel:  "system",
		SenderID: "subagent",
		ChatID:   fmt.Sprintf("%s:%s", originChannel, originChatID),
		Content:  content,
	}

	m.bus.PublishInbound(msg)
	m.logger.Debug("子代理结果已回报",
		zap.String("id", taskID),
		zap.String("channel", originChannel),
	)
}

// buildSubagentPrompt 构建子代理系统 prompt (对标 subagent.py:_build_subagent_prompt)
func (m *SubagentManager) buildSubagentPrompt() string {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	tz := time.Now().Format("MST")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`# Subagent

## Current Time
%s (%s)

You are a subagent spawned by the main agent to complete a specific task.

## Rules
1. Stay focused - complete only the assigned task, nothing else
2. Your final response will be reported back to the main agent
3. Do not initiate conversations or take on side tasks
4. Be concise but informative in your findings

## What You Can Do
- Read and write files in the workspace
- Execute shell commands
- Search the web and fetch web pages
- Complete the task thoroughly

## What You Cannot Do
- Send messages directly to users (no message tool available)
- Spawn other subagents
- Access the main agent's conversation history

## Workspace
Your workspace is at: %s
Skills are available at: %s/skills/ (read SKILL.md files as needed)

When you have completed the task, provide a clear summary of your findings or actions.`,
		now, tz, m.workspace, m.workspace))

	if m.skillsSummary != "" {
		sb.WriteString("\n\n## Available Skills\n")
		sb.WriteString(m.skillsSummary)
	}

	return sb.String()
}

// GetRunningCount 获取运行中的子代理数量
func (m *SubagentManager) GetRunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runningTasks)
}
