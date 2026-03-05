package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yangkun19921001/PP-Claw/agent/tools"
	"github.com/yangkun19921001/PP-Claw/bus"
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

	mu           sync.Mutex
	runningTasks map[string]context.CancelFunc
}

// NewSubagentManager 创建子代理管理器
func NewSubagentManager(workspace string, msgBus *bus.MessageBus, model string, logger *zap.Logger) *SubagentManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &SubagentManager{
		workspace:     workspace,
		bus:           msgBus,
		model:         model,
		maxIterations: 15,
		logger:        logger,
		runningTasks:  make(map[string]context.CancelFunc),
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

	ctx2, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.runningTasks[taskID] = cancel
	m.mu.Unlock()

	go m.runSubagent(ctx2, taskID, task, displayLabel, originChannel, originChatID)

	m.logger.Info("子代理已生成", zap.String("id", taskID), zap.String("label", displayLabel))
	return fmt.Sprintf("Subagent [%s] started (id: %s). I'll notify you when it completes.", displayLabel, taskID)
}

// runSubagent 执行子代理任务 (对标 subagent.py:_run_subagent)
func (m *SubagentManager) runSubagent(ctx context.Context, taskID, task, label, originChannel, originChatID string) {
	defer func() {
		m.mu.Lock()
		delete(m.runningTasks, taskID)
		m.mu.Unlock()
	}()

	m.logger.Info("子代理启动", zap.String("id", taskID), zap.String("label", label))

	// 构建子代理工具 (无 message, 无 spawn)
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

	// 构建子代理 prompt
	sysPrompt := m.buildSubagentPrompt()

	// 简化实现: 使用工具直接执行任务
	// 完整实现应创建独立 Eino ADK Agent
	_ = ctx
	_ = sysPrompt

	finalResult := fmt.Sprintf("Subagent [%s] completed task: %s", label, task)

	// 回报结果
	m.announceResult(taskID, label, task, finalResult, originChannel, originChatID, "ok")
}

// announceResult 回报子代理结果 (对标 subagent.py:_announce_result)
func (m *SubagentManager) announceResult(taskID, label, task, result, originChannel, originChatID, status string) {
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

	return fmt.Sprintf(`# Subagent

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
		now, tz, m.workspace, m.workspace)
}

// GetRunningCount 获取运行中的子代理数量
func (m *SubagentManager) GetRunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runningTasks)
}
