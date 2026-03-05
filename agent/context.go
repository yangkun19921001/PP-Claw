package agent

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ContextBuilder 构建 Agent 上下文 (对标 pp-claw/agent/context.py:ContextBuilder)
type ContextBuilder struct {
	Workspace string
}

// NewContextBuilder 创建上下文构建器
func NewContextBuilder(workspace string) *ContextBuilder {
	return &ContextBuilder{Workspace: workspace}
}

// BootstrapFiles 引导文件列表 (与 pp-claw 相同)
var BootstrapFiles = []string{"AGENTS.md", "SOUL.md", "USER.md", "TOOLS.md", "IDENTITY.md"}

// BuildSystemPrompt 构建系统提示词 (对标 context.py:build_system_prompt)
func (c *ContextBuilder) BuildSystemPrompt() string {
	var parts []string

	// 核心身份
	parts = append(parts, c.getIdentity())

	// 引导文件
	if bootstrap := c.loadBootstrapFiles(); bootstrap != "" {
		parts = append(parts, bootstrap)
	}

	// 记忆上下文
	memoryCtx := c.getMemoryContext()
	if memoryCtx != "" {
		parts = append(parts, fmt.Sprintf("# Memory\n\n%s", memoryCtx))
	}

	// Always-loaded 技能 (对标 context.py: always_skills)
	skillsLoader := NewSkillsLoader(c.Workspace)
	alwaysSkills := skillsLoader.GetAlwaysSkills()
	if len(alwaysSkills) > 0 {
		alwaysContent := skillsLoader.LoadSkillsForContext(alwaysSkills)
		if alwaysContent != "" {
			parts = append(parts, fmt.Sprintf("# Active Skills\n\n%s", alwaysContent))
		}
	}

	// 技能摘要 (progressive loading)
	skillsSummary := skillsLoader.BuildSkillsSummary()
	if skillsSummary != "" {
		parts = append(parts, fmt.Sprintf(`# Skills

The following skills extend your capabilities. To use a skill, read its SKILL.md file using the read_file tool.
Skills with available="false" need dependencies installed first - you can try installing them with apt/brew.

%s`, skillsSummary))
	}

	return strings.Join(parts, "\n\n---\n\n")
}

// getIdentity 获取核心身份 (对标 context.py:_get_identity)
func (c *ContextBuilder) getIdentity() string {
	ws, _ := filepath.Abs(c.Workspace)
	osName := runtime.GOOS
	arch := runtime.GOARCH
	if osName == "darwin" {
		osName = "macOS"
	}

	return fmt.Sprintf(`# pp-claw 🦞

You are pp-claw, a helpful AI assistant. 

## Runtime
%s %s, Go %s

## Workspace
Your workspace is at: %s
- Long-term memory: %s/memory/MEMORY.md
- History log: %s/memory/HISTORY.md (grep-searchable)
- Custom skills: %s/skills/{skill-name}/SKILL.md

Reply directly with text for conversations. Only use the 'message' tool to send to a specific chat channel.

## Tool Call Guidelines
- Before calling tools, you may briefly state your intent (e.g. "Let me check that"), but NEVER predict or describe the expected result before receiving it.
- Before modifying a file, read it first to confirm its current content.
- Do not assume a file or directory exists — use list_directory or read_file to verify.
- After writing or editing a file, re-read it if accuracy matters.
- If a tool call fails, analyze the error before retrying with a different approach.

## Memory
- Remember important facts: write to %s/memory/MEMORY.md
- Recall past events: grep %s/memory/HISTORY.md`,
		osName, arch, runtime.Version(),
		ws, ws, ws, ws, ws, ws)
}

// loadBootstrapFiles 加载引导文件 (对标 context.py:_load_bootstrap_files)
func (c *ContextBuilder) loadBootstrapFiles() string {
	var parts []string
	for _, filename := range BootstrapFiles {
		path := filepath.Join(c.Workspace, filename)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("## %s\n\n%s", filename, string(data)))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// getMemoryContext 获取记忆上下文
func (c *ContextBuilder) getMemoryContext() string {
	memoryFile := filepath.Join(c.Workspace, "memory", "MEMORY.md")
	data, err := os.ReadFile(memoryFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// getSkillsSummary 获取技能摘要
func (c *ContextBuilder) getSkillsSummary() string {
	skillsDir := filepath.Join(c.Workspace, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillFile := filepath.Join(skillsDir, e.Name(), "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", e.Name(), skillFile))
	}
	return sb.String()
}

// BuildMessages 构建完整消息列表 (对标 context.py:build_messages)
func (c *ContextBuilder) BuildMessages(
	history []map[string]any,
	currentMessage string,
	media []string,
	channel string,
	chatID string,
) []map[string]any {
	var messages []map[string]any

	// 系统提示词
	messages = append(messages, map[string]any{
		"role":    "system",
		"content": c.BuildSystemPrompt(),
	})

	// 历史消息
	messages = append(messages, history...)

	// 当前用户消息 (包含图片和运行时上下文)
	userContent := c.buildUserContent(currentMessage, media)
	userContent = c.injectRuntimeContext(userContent, channel, chatID)
	messages = append(messages, map[string]any{
		"role":    "user",
		"content": userContent,
	})

	return messages
}

// buildUserContent 构建用户消息内容 (支持 base64 图片, 对标 context.py:_build_user_content)
func (c *ContextBuilder) buildUserContent(text string, media []string) any {
	if len(media) == 0 {
		return text
	}

	var images []map[string]any
	for _, path := range media {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		ext := strings.ToLower(filepath.Ext(path))
		mimeType := mime.TypeByExtension(ext)
		if mimeType == "" || !strings.HasPrefix(mimeType, "image/") {
			continue
		}
		b64 := base64.StdEncoding.EncodeToString(data)
		images = append(images, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": fmt.Sprintf("data:%s;base64,%s", mimeType, b64),
			},
		})
	}

	if len(images) == 0 {
		return text
	}

	// 多模态消息: images + text
	result := make([]map[string]any, 0, len(images)+1)
	result = append(result, images...)
	result = append(result, map[string]any{"type": "text", "text": text})
	return result
}

// injectRuntimeContext 注入运行时上下文 (对标 context.py:_inject_runtime_context)
// 支持 string 和 []map[string]any (多模态) 两种输入类型
func (c *ContextBuilder) injectRuntimeContext(content any, channel, chatID string) any {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	tz := time.Now().Format("MST")

	lines := []string{fmt.Sprintf("Current Time: %s (%s)", now, tz)}
	if channel != "" && chatID != "" {
		lines = append(lines, fmt.Sprintf("Channel: %s", channel))
		lines = append(lines, fmt.Sprintf("Chat ID: %s", chatID))
	}

	block := "[Runtime Context]\n" + strings.Join(lines, "\n")

	switch v := content.(type) {
	case string:
		return v + "\n\n" + block
	case []map[string]any:
		return append(v, map[string]any{"type": "text", "text": block})
	default:
		return content
	}
}

// AddToolResult 添加工具结果到消息列表
func (c *ContextBuilder) AddToolResult(messages []map[string]any, toolCallID, toolName, result string) []map[string]any {
	return append(messages, map[string]any{
		"role":         "tool",
		"tool_call_id": toolCallID,
		"name":         toolName,
		"content":      result,
	})
}

// AddAssistantMessage 添加助手消息 (对标 context.py:add_assistant_message, 支持 reasoning_content)
func (c *ContextBuilder) AddAssistantMessage(messages []map[string]any, content string, toolCalls []map[string]any, reasoningContent string) []map[string]any {
	msg := map[string]any{
		"role":    "assistant",
		"content": content,
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	if reasoningContent != "" {
		msg["reasoning_content"] = reasoningContent
	}
	return append(messages, msg)
}
