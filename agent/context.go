package agent

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
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
When you need to send media files (images, audio, video, documents), you MUST use the 'message' tool with the 'media' parameter containing the file paths. Direct text replies cannot carry media attachments.

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
// 如果文件不存在，自动从内嵌资源释放到 workspace
func (c *ContextBuilder) loadBootstrapFiles() string {
	// 首次启动: 从 embedded 释放 templates 到 workspace
	c.ensureBootstrapFiles()

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

// ensureBootstrapFiles 确保引导文件存在
// 如果 workspace 中不存在，从内嵌的 templates/ 释放
func (c *ContextBuilder) ensureBootstrapFiles() {
	if !hasEmbeddedAssets {
		return
	}

	for _, filename := range BootstrapFiles {
		targetPath := filepath.Join(c.Workspace, filename)
		// 如果已存在则跳过 (不覆盖用户修改)
		if _, err := os.Stat(targetPath); err == nil {
			continue
		}
		// 从 embedded templates 读取
		embeddedPath := "templates/" + filename
		data, err := fs.ReadFile(embeddedTemplatesFS, embeddedPath)
		if err != nil {
			continue
		}
		// 确保目录存在
		os.MkdirAll(filepath.Dir(targetPath), 0755)
		os.WriteFile(targetPath, data, 0644)
	}

	// 释放 memory/MEMORY.md 模板
	memoryTarget := filepath.Join(c.Workspace, "memory", "MEMORY.md")
	if _, err := os.Stat(memoryTarget); err != nil {
		if data, err := fs.ReadFile(embeddedTemplatesFS, "templates/memory/MEMORY.md"); err == nil {
			os.MkdirAll(filepath.Dir(memoryTarget), 0755)
			os.WriteFile(memoryTarget, data, 0644)
		}
	}
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

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".bmp": true, ".svg": true, ".ico": true,
}

var textExts = map[string]bool{
	".txt": true, ".md": true, ".csv": true, ".tsv": true,
	".json": true, ".xml": true, ".yaml": true, ".yml": true,
	".toml": true, ".ini": true, ".cfg": true, ".conf": true,
	".log": true, ".sh": true, ".bash": true, ".zsh": true,
	".py": true, ".go": true, ".js": true, ".ts": true,
	".java": true, ".c": true, ".cpp": true, ".h": true,
	".rs": true, ".rb": true, ".php": true, ".sql": true,
	".html": true, ".css": true, ".scss": true, ".less": true,
	".jsx": true, ".tsx": true, ".vue": true, ".svelte": true,
}

func isTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if textExts[ext] {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return false
	}
	sample := data
	if len(sample) > 1024 {
		sample = sample[:1024]
	}
	for _, b := range sample {
		if b == 0 {
			return false
		}
	}
	return true
}

// buildUserContent 构建用户消息内容。
// 图片文件转为 base64 多模态 image_url 块，非图片文件内容作为文本附加。
func (c *ContextBuilder) buildUserContent(text string, media []string) any {
	if len(media) == 0 {
		return text
	}

	var imageParts []map[string]any
	var fileParts []string

	for _, path := range media {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		ext := strings.ToLower(filepath.Ext(path))
		if imageExts[ext] {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			mimeType := mime.TypeByExtension(ext)
			if mimeType == "" {
				mimeType = "image/png"
			}
			dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
			imageParts = append(imageParts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": dataURL},
			})
		} else if isTextFile(path) {
			data, err := os.ReadFile(path)
			if err != nil {
				fileParts = append(fileParts, fmt.Sprintf("[File: %s] (unreadable)", filepath.Base(path)))
				continue
			}
			content := string(data)
			if len(content) > 10000 {
				content = content[:10000] + "\n... (truncated)"
			}
			fileParts = append(fileParts, fmt.Sprintf("[File: %s]\n%s", filepath.Base(path), content))
		} else {
			fileParts = append(fileParts, fmt.Sprintf("[Attached File: %s]\nPath: %s\nUse tools (read_file, execute) to process this file.", filepath.Base(path), path))
		}
	}

	if len(imageParts) == 0 && len(fileParts) == 0 {
		return text
	}

	// 有非图片文件时，追加到文本
	msgText := text
	if len(fileParts) > 0 {
		msgText = msgText + "\n\n" + strings.Join(fileParts, "\n\n")
	}

	// 无图片时直接返回文本
	if len(imageParts) == 0 {
		return strings.TrimSpace(msgText)
	}

	// 有图片时返回多模态内容
	parts := []map[string]any{{"type": "text", "text": strings.TrimSpace(msgText)}}
	parts = append(parts, imageParts...)
	return parts
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

// buildEinoMultiContent 将 map 格式的多模态内容转换为 Eino schema 的 MessageInputPart 列表。
func buildEinoMultiContent(parts []map[string]any, extraText string) []schema.MessageInputPart {
	var result []schema.MessageInputPart
	for _, p := range parts {
		typ, _ := p["type"].(string)
		switch typ {
		case "text":
			txt, _ := p["text"].(string)
			result = append(result, schema.MessageInputPart{
				Type: schema.ChatMessagePartTypeText,
				Text: txt,
			})
		case "image_url":
			if iu, ok := p["image_url"].(map[string]any); ok {
				url, _ := iu["url"].(string)
				result = append(result, schema.MessageInputPart{
					Type: schema.ChatMessagePartTypeImageURL,
					Image: &schema.MessageInputImage{
						MessagePartCommon: schema.MessagePartCommon{URL: &url},
					},
				})
			}
		}
	}
	if extraText != "" {
		result = append(result, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeText,
			Text: extraText,
		})
	}
	return result
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
