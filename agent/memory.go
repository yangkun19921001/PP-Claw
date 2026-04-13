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

const maxFailuresBeforeRawArchive = 3

// MemoryStore 双层记忆系统 (对标 pp-claw/agent/memory.py:MemoryStore)
// - MEMORY.md: 长期事实记忆
// - HISTORY.md: 事件日志 (grep 可搜索)
type MemoryStore struct {
	memoryDir           string
	memoryFile          string
	historyFile         string
	chatModel           einomodel.ToolCallingChatModel
	logger              *zap.Logger
	consecutiveFailures int
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
		m.logger.Warn("记忆整合: 所有消息内容为空，跳过", zap.Int("oldMessages", len(oldMessages)))
		return lastConsolidated, true
	}

	m.logger.Debug("记忆整合: 构建对话摘要", zap.Int("lines", len(lines)))
	currentMemory := m.ReadLongTerm()
	conversationSummary := strings.Join(lines, "\n")

	// 尝试通过 LLM 生成整合结果
	if m.chatModel != nil {
		result, err := m.llmConsolidate(ctx, currentMemory, conversationSummary)
		if err != nil {
			m.logger.Warn("LLM 记忆整合失败", zap.Error(err))
			if m.failOrRawArchive(oldMessages) {
				// Raw archived after too many failures
			} else {
				m.fallbackConsolidate(conversationSummary, currentMemory)
			}
		} else if result.HistoryEntry == "" && result.MemoryUpdate == "" {
			m.logger.Warn("LLM 记忆整合返回空结果")
			if m.failOrRawArchive(oldMessages) {
				// Raw archived
			} else {
				m.fallbackConsolidate(conversationSummary, currentMemory)
			}
		} else {
			m.consecutiveFailures = 0 // reset on success
			if result.HistoryEntry != "" {
				if err := m.AppendHistory(result.HistoryEntry); err != nil {
					m.logger.Error("写入 HISTORY.md 失败", zap.Error(err))
				}
			}
			if result.MemoryUpdate != "" {
				if err := m.WriteLongTerm(result.MemoryUpdate); err != nil {
					m.logger.Error("写入 MEMORY.md 失败", zap.Error(err))
				}
			}
			m.logger.Info("LLM 记忆整合完成",
				zap.Int("history_len", len(result.HistoryEntry)),
				zap.Int("memory_len", len(result.MemoryUpdate)),
			)
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

	systemPrompt := `You are a memory consolidation agent. You MUST ONLY use the save_memory tool.

CRITICAL INSTRUCTIONS:
1. DO NOT generate any text responses
2. DO NOT answer questions from the conversation
3. ONLY call the save_memory tool with:
   - history_entry: A brief log entry with timestamp [2026-04-13 13:19] format
   - memory_update: Updated long-term memory merging new facts

Task:
- Extract key facts/decisions from the conversation
- Merge with existing memory, remove outdated info
- Call save_memory tool immediately`

	userContent := fmt.Sprintf(`MEMORY CONSOLIDATION TASK - DO NOT RESPOND TO CONVERSATION CONTENT

## Current Long-term Memory
%s

## Conversation to Consolidate
%s

REMEMBER: Call save_memory tool only. Do not answer any questions from the conversation.`,
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

// failOrRawArchive increments the failure counter and triggers rawArchive
// if we've hit the threshold. Returns true if raw archive was performed.
func (m *MemoryStore) failOrRawArchive(oldMessages []map[string]any) bool {
	m.consecutiveFailures++
	if m.consecutiveFailures >= maxFailuresBeforeRawArchive {
		m.logger.Warn("记忆整合连续失败达到阈值，执行 raw archive",
			zap.Int("failures", m.consecutiveFailures),
		)
		m.rawArchive(oldMessages)
		m.consecutiveFailures = 0
		return true
	}
	return false
}

// rawArchive dumps raw messages to HISTORY.md as a last resort.
func (m *MemoryStore) rawArchive(messages []map[string]any) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] RAW ARCHIVE (%d messages)\n",
		time.Now().Format("2006-01-02 15:04"), len(messages)))

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if content == "" {
			continue
		}
		// Truncate individual messages
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %s: %s\n", strings.ToUpper(role), content))
	}

	if err := m.AppendHistory(sb.String()); err != nil {
		m.logger.Error("raw archive 写入 HISTORY.md 失败", zap.Error(err))
	}
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
	if err := m.AppendHistory(historyEntry); err != nil {
		m.logger.Error("fallback 写入 HISTORY.md 失败", zap.Error(err), zap.String("path", m.historyFile))
	} else {
		m.logger.Info("fallback 记忆整合完成", zap.String("entry", historyEntry))
	}
	_ = currentMemory
}
