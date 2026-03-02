package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// MemoryStore 双层记忆系统 (对标 nanobot/agent/memory.py:MemoryStore)
// - MEMORY.md: 长期事实记忆
// - HISTORY.md: 事件日志 (grep 可搜索)
type MemoryStore struct {
	memoryDir   string
	memoryFile  string
	historyFile string
	chatModel   einomodel.ToolCallingChatModel
	logger      *zap.Logger
}

// NewMemoryStore 创建记忆存储
func NewMemoryStore(workspace string, chatModel einomodel.ToolCallingChatModel, logger *zap.Logger) *MemoryStore {
	memDir := filepath.Join(workspace, "memory")
	os.MkdirAll(memDir, 0755)

	if logger == nil {
		logger = zap.NewNop()
	}

	return &MemoryStore{
		memoryDir:   memDir,
		memoryFile:  filepath.Join(memDir, "MEMORY.md"),
		historyFile: filepath.Join(memDir, "HISTORY.md"),
		chatModel:   chatModel,
		logger:      logger,
	}
}

// ReadLongTerm 读取长期记忆
func (m *MemoryStore) ReadLongTerm() string {
	data, err := os.ReadFile(m.memoryFile)
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteLongTerm 写入长期记忆
func (m *MemoryStore) WriteLongTerm(content string) error {
	return os.WriteFile(m.memoryFile, []byte(content), 0644)
}

// AppendHistory 追加历史条目
func (m *MemoryStore) AppendHistory(entry string) error {
	f, err := os.OpenFile(m.historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strings.TrimRight(entry, "\n") + "\n\n")
	return err
}

// GetMemoryContext 获取记忆上下文 (注入 System Prompt)
func (m *MemoryStore) GetMemoryContext() string {
	longTerm := m.ReadLongTerm()
	if longTerm == "" {
		return ""
	}
	return fmt.Sprintf("## Long-term Memory\n%s", longTerm)
}

// saveMemoryToolInfo 定义 save_memory 工具的 schema
func saveMemoryToolInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "save_memory",
		Desc: "Save consolidated memory. Call this tool with a history_entry (short log line) and memory_update (updated long-term memory content).",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"history_entry": {
				Type:     schema.String,
				Desc:     "A concise one-line summary for the history log, prefixed with a timestamp like [2025-01-01 12:00].",
				Required: true,
			},
			"memory_update": {
				Type:     schema.String,
				Desc:     "The updated long-term memory content (full replacement). Merge new facts with existing memory, remove outdated info.",
				Required: true,
			},
		}),
	}
}

// saveMemoryResult 解析 save_memory 工具调用的参数
type saveMemoryResult struct {
	HistoryEntry string `json:"history_entry"`
	MemoryUpdate string `json:"memory_update"`
}

// Consolidate 通过 LLM tool call 整合记忆 (对标 memory.py:consolidate)
// 将旧消息摘要写入 MEMORY.md + HISTORY.md
func (m *MemoryStore) Consolidate(ctx context.Context, sessionMessages []map[string]any, archiveAll bool, memoryWindow int, lastConsolidated int) (int, bool) {
	var oldMessages []map[string]any
	var keepCount int

	totalMessages := len(sessionMessages)

	if archiveAll {
		oldMessages = sessionMessages
		keepCount = 0
		m.logger.Info("记忆整合 (archive_all)", zap.Int("messages", totalMessages))
	} else {
		keepCount = memoryWindow / 2
		if totalMessages <= keepCount {
			return lastConsolidated, true
		}
		if totalMessages-lastConsolidated <= 0 {
			return lastConsolidated, true
		}
		endIdx := totalMessages - keepCount
		if lastConsolidated >= endIdx {
			return lastConsolidated, true
		}
		oldMessages = sessionMessages[lastConsolidated:endIdx]
		if len(oldMessages) == 0 {
			return lastConsolidated, true
		}
		m.logger.Info("记忆整合", zap.Int("to_consolidate", len(oldMessages)), zap.Int("keep", keepCount))
	}

	// 构建对话摘要
	var lines []string
	for _, msg := range oldMessages {
		content, _ := msg["content"].(string)
		if content == "" {
			continue
		}
		role, _ := msg["role"].(string)
		timestamp, _ := msg["timestamp"].(string)
		if len(timestamp) > 16 {
			timestamp = timestamp[:16]
		}
		if timestamp == "" {
			timestamp = "?"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", timestamp, strings.ToUpper(role), content))
	}

	if len(lines) == 0 {
		return lastConsolidated, true
	}

	currentMemory := m.ReadLongTerm()
	conversationSummary := strings.Join(lines, "\n")

	// 尝试通过 LLM 生成整合结果
	if m.chatModel != nil {
		result, err := m.llmConsolidate(ctx, currentMemory, conversationSummary)
		if err != nil {
			m.logger.Warn("LLM 记忆整合失败，使用 fallback", zap.Error(err))
			m.fallbackConsolidate(conversationSummary, currentMemory)
		} else {
			if result.HistoryEntry != "" {
				m.AppendHistory(result.HistoryEntry)
			}
			if result.MemoryUpdate != "" {
				m.WriteLongTerm(result.MemoryUpdate)
			}
		}
	} else {
		m.fallbackConsolidate(conversationSummary, currentMemory)
	}

	if archiveAll {
		return 0, true
	}
	return totalMessages - keepCount, true
}

// llmConsolidate 使用 LLM tool call 执行记忆整合
func (m *MemoryStore) llmConsolidate(ctx context.Context, currentMemory, conversationSummary string) (*saveMemoryResult, error) {
	toolInfo := saveMemoryToolInfo()

	// 使用 WithTools 注入 save_memory 工具
	boundModel, err := m.chatModel.WithTools([]*schema.ToolInfo{toolInfo})
	if err != nil {
		return nil, fmt.Errorf("bind tools failed: %w", err)
	}

	systemPrompt := `You are a memory consolidation agent. Your job is to:
1. Review the conversation below and extract key facts, decisions, and outcomes.
2. Merge them with the existing long-term memory, removing outdated or contradicted info.
3. Create a concise history log entry summarizing what happened.

You MUST call the save_memory tool with your results. Do not respond with text.`

	userContent := fmt.Sprintf("## Current Long-term Memory\n%s\n\n## Conversation to Consolidate\n%s",
		currentMemory, conversationSummary)

	messages := []*schema.Message{
		{Role: schema.System, Content: systemPrompt},
		{Role: schema.User, Content: userContent},
	}

	resp, err := boundModel.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM generate failed: %w", err)
	}

	if len(resp.ToolCalls) == 0 {
		return nil, fmt.Errorf("LLM did not call save_memory tool")
	}

	var result saveMemoryResult
	if err := json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &result); err != nil {
		return nil, fmt.Errorf("parse tool call arguments failed: %w", err)
	}

	return &result, nil
}

// fallbackConsolidate 简化的 fallback 整合（无 LLM 时使用）
func (m *MemoryStore) fallbackConsolidate(conversationSummary, currentMemory string) {
	timestampStr := "[" + time.Now().Format("2006-01-02 15:04") + "]"
	entryLines := strings.Split(conversationSummary, "\n")
	historyEntry := timestampStr + " "
	for _, line := range entryLines {
		if strings.Contains(line, "USER:") {
			parts := strings.SplitN(line, "USER: ", 2)
			if len(parts) > 1 {
				topic := parts[1]
				if len(topic) > 200 {
					topic = topic[:200] + "..."
				}
				historyEntry += topic
				break
			}
		}
	}
	m.AppendHistory(historyEntry)
	_ = currentMemory
}
