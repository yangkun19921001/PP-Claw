// Package learning - 技能提取器的具体实现
// 遵循 Google Go 编程规范，实现 SkillExtractor 接口
package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// DefaultSkillExtractor 默认技能提取器实现
// 使用 LLM 分析会话历史，识别可重用的技能模式
type DefaultSkillExtractor struct {
	model        einomodel.ToolCallingChatModel // LLM 客户端
	logger       *zap.Logger                    // 日志器
	config       *SkillExtractionConfig         // 配置
	reviewPrompt string                         // 技能审查提示词
}

// SkillExtractionConfig 技能提取配置
type SkillExtractionConfig struct {
	ReviewInterval        int    // 触发技能审查的工具调用间隔
	MinConversationLength int    // 最小对话长度
	MinToolCalls          int    // 最小工具调用次数
	ExtractorModel        string // 提取器使用的模型
	ReviewPrompt          string // 自定义技能审查提示词
}

// NewDefaultSkillExtractor 创建默认技能提取器
func NewDefaultSkillExtractor(
	model einomodel.ToolCallingChatModel,
	logger *zap.Logger,
	config *SkillExtractionConfig,
) *DefaultSkillExtractor {
	if config == nil {
		config = &SkillExtractionConfig{
			ReviewInterval:        10, // 每10次工具调用检查一次
			MinConversationLength: 5,  // 至少5轮对话
			MinToolCalls:          3,  // 至少3次工具调用
			ExtractorModel:        "", // 使用默认模型
			ReviewPrompt:          "", // 使用内置提示词
		}
	}

	reviewPrompt := buildSkillReviewPrompt()
	if strings.TrimSpace(config.ReviewPrompt) != "" {
		reviewPrompt = config.ReviewPrompt
	}

	return &DefaultSkillExtractor{
		model:        model,
		logger:       logger,
		config:       config,
		reviewPrompt: reviewPrompt,
	}
}

// ExtractSkill 实现 SkillExtractor.ExtractSkill
// 分析对话历史，识别可重用的技能模式
func (e *DefaultSkillExtractor) ExtractSkill(
	ctx context.Context,
	history []ConversationTurn,
) (*Skill, error) {
	if !e.ShouldExtract(history, e.countToolCalls(history)) {
		return nil, nil
	}

	// 构建分析提示词
	analysisPrompt := e.buildAnalysisPrompt(history)

	// 调用 LLM 进行技能提取
	messages := []*schema.Message{
		{Role: schema.User, Content: analysisPrompt},
	}

	response, err := e.model.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM 技能提取失败: %w", err)
	}

	// 解析 LLM 响应
	skill, err := e.parseSkillResponse(response.Content)
	if err != nil {
		e.logger.Warn("技能响应解析失败",
			zap.Error(err),
			zap.String("response_preview", previewSkillResponse(response.Content, 240)),
		)
		return nil, fmt.Errorf("解析技能响应失败: %w", err)
	}

	// 如果 LLM 认为没有值得保存的技能
	if skill == nil {
		e.logger.Debug("LLM 判断无值得保存的技能")
		return nil, nil
	}

	// 设置技能元数据
	skill.CreatedAt = time.Now()
	skill.UpdatedAt = time.Now()
	skill.Version = "1.0.0"
	skill.UsageCount = 0
	skill.SuccessRate = 0.0

	e.logger.Info("成功提取技能",
		zap.String("skill_name", skill.Name),
		zap.String("description", skill.Description),
		zap.Int("conversation_length", len(history)),
	)

	return skill, nil
}

// ShouldExtract 实现 SkillExtractor.ShouldExtract
// 判断当前会话是否满足技能提取的条件
func (e *DefaultSkillExtractor) ShouldExtract(
	history []ConversationTurn,
	toolUsageCount int,
) bool {
	// 检查对话长度
	if len(history) < e.config.MinConversationLength {
		return false
	}

	// 检查工具调用次数
	if toolUsageCount < e.config.MinToolCalls {
		return false
	}

	// 检查是否包含复杂的问题解决过程
	return e.hasComplexProblemSolving(history)
}

// countToolCalls 统计历史中的工具调用次数
func (e *DefaultSkillExtractor) countToolCalls(history []ConversationTurn) int {
	count := 0
	for _, turn := range history {
		count += len(turn.ToolCalls)
	}
	return count
}

// hasComplexProblemSolving 检查会话是否包含复杂的问题解决过程
func (e *DefaultSkillExtractor) hasComplexProblemSolving(history []ConversationTurn) bool {
	toolCallCount := 0
	errorRecoveryCount := 0
	iterations := 0

	for _, turn := range history {
		if len(turn.ToolCalls) > 0 {
			toolCallCount++
			iterations++

			// 检查是否有错误恢复
			for _, toolCall := range turn.ToolCalls {
				if !toolCall.Success {
					errorRecoveryCount++
				}
			}
		}
	}

	// 复杂问题解决的特征：
	// 1. 多次工具调用
	// 2. 有错误恢复
	// 3. 多轮迭代
	return toolCallCount >= 3 && (errorRecoveryCount > 0 || iterations >= 3)
}

// buildAnalysisPrompt 构建技能分析提示词
func (e *DefaultSkillExtractor) buildAnalysisPrompt(history []ConversationTurn) string {
	var sb strings.Builder

	// 添加会话历史
	sb.WriteString("请分析以下对话历史，判断是否包含可重用的技能模式:\n\n")

	for i, turn := range history {
		sb.WriteString(fmt.Sprintf("=== Turn %d (%s) ===\n", i+1, turn.Role))
		sb.WriteString(turn.Content)
		sb.WriteString("\n")

		if len(turn.ToolCalls) > 0 {
			sb.WriteString("工具调用:\n")
			for _, toolCall := range turn.ToolCalls {
				status := "成功"
				if !toolCall.Success {
					status = "失败"
				}
				sb.WriteString(fmt.Sprintf("- %s(%v) -> %s [%s]\n",
					toolCall.Name, toolCall.Args, toolCall.Result, status))
			}
		}
		sb.WriteString("\n")
	}

	// 添加提示词
	sb.WriteString(e.reviewPrompt)

	return sb.String()
}

// parseSkillResponse 解析 LLM 的技能提取响应
func (e *DefaultSkillExtractor) parseSkillResponse(response string) (*Skill, error) {
	response = normalizeSkillResponse(response)
	if response == "" {
		return nil, nil
	}

	// 如果明确表示没有技能
	if isNoSkillResponse(response) {
		return nil, nil
	}

	// 尝试解析 JSON 格式的技能定义
	if strings.Contains(response, "{") && strings.Contains(response, "}") {
		skill, err := e.parseJSONSkill(response)
		if err == nil {
			return skill, nil
		}
	}

	// 解析自然语言格式的技能定义
	skill, err := e.parseNaturalLanguageSkill(response)
	if err == nil {
		return skill, nil
	}

	// 模型偶尔会返回自由文本分析，而不是严格的结构化结果。
	// 这类响应不应记录为错误，只视为“没有可保存技能”。
	if !looksLikeSkillDefinition(response) {
		return nil, nil
	}

	if isSoftSkillParseError(err) {
		return nil, nil
	}

	return nil, err
}

// parseJSONSkill 解析 JSON 格式的技能
func (e *DefaultSkillExtractor) parseJSONSkill(response string) (*Skill, error) {
	// 提取 JSON 部分
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}") + 1
	if start == -1 || end <= start {
		return nil, fmt.Errorf("未找到有效的 JSON")
	}

	jsonStr := response[start:end]

	var raw map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}

	skill := &Skill{
		Tags:     []string{},
		Metadata: make(map[string]any),
	}
	skill.Name = firstNonEmptyString(raw,
		"name", "skill_name", "skillName", "skill", "title", "标题", "技能名称", "技能名", "技能", "名称",
	)
	skill.Description = firstNonEmptyString(raw,
		"description", "desc", "summary", "描述",
	)
	skill.Content = firstNonEmptyString(raw,
		"content", "instruction", "instructions", "body", "内容",
	)
	skill.Tags = extractStringSlice(raw,
		"tags", "tag", "labels", "标签",
	)

	if skill.Name == "" || skill.Content == "" {
		return nil, fmt.Errorf("技能名称或内容为空")
	}
	if skill.Description == "" {
		skill.Description = "从会话中自动提取的技能"
	}

	return skill, nil
}

// parseNaturalLanguageSkill 解析自然语言格式的技能
func (e *DefaultSkillExtractor) parseNaturalLanguageSkill(response string) (*Skill, error) {
	lines := strings.Split(response, "\n")

	skill := &Skill{
		Tags:     []string{},
		Metadata: make(map[string]any),
	}

	var currentSection string
	var contentLines []string
	var descriptionLines []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || isCodeFenceLine(line) {
			continue
		}

		// 识别各个部分
		if section, value, ok := parseSkillFieldLine(line); ok {
			currentSection = section
			switch section {
			case "name":
				if value != "" {
					skill.Name = value
				}
			case "description":
				if value != "" {
					descriptionLines = append(descriptionLines, value)
				}
			case "content":
				if value != "" {
					contentLines = append(contentLines, value)
				}
			case "tags":
				skill.Tags = splitTags(value)
			}
			continue
		}

		if section, ok := parseSkillSectionHeading(line); ok {
			currentSection = section
			continue
		}

		switch currentSection {
		case "name":
			if skill.Name == "" {
				skill.Name = cleanInlineMarkdown(line)
			}
		case "description":
			descriptionLines = append(descriptionLines, cleanInlineMarkdown(line))
		case "content":
			contentLines = append(contentLines, line)
		case "tags":
			skill.Tags = append(skill.Tags, splitTags(line)...)
		}
	}

	skill.Content = strings.Join(contentLines, "\n")
	skill.Description = strings.Join(descriptionLines, "\n")

	// 验证必要字段
	if skill.Name == "" {
		return nil, fmt.Errorf("技能名称为空")
	}
	if skill.Content == "" {
		return nil, fmt.Errorf("技能内容为空")
	}
	if skill.Description == "" {
		skill.Description = "从会话中自动提取的技能"
	}

	return skill, nil
}

func normalizeSkillResponse(response string) string {
	response = strings.ReplaceAll(response, "\r\n", "\n")
	response = strings.ReplaceAll(response, "\r", "\n")
	return strings.TrimSpace(response)
}

func isNoSkillResponse(response string) bool {
	normalized := strings.ToLower(normalizeSkillResponse(response))
	phrases := []string{
		"nothing to save",
		"nothing worth saving",
		"no reusable skill",
		"no reusable skills",
		"no reusable pattern",
		"no new skill",
		"无技能值得保存",
		"没有技能",
		"没有值得保存",
		"没有可复用",
		"未发现值得保存",
		"未发现可复用",
		"无需保存",
		"不需要保存",
		"不值得保存",
		"当前对话不包含值得保存",
	}
	for _, phrase := range phrases {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}

func looksLikeSkillDefinition(response string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(normalizeSkillResponse(response), "：", ":"))
	markers := []string{
		"技能名称",
		"技能名",
		"技能:",
		"名称:",
		"name:",
		"skill:",
		"skill name",
		"title:",
		"标题:",
		"描述:",
		"description:",
		"内容:",
		"content:",
		"标签:",
		"tags:",
		"\"name\"",
		"\"content\"",
		"\"技能名称\"",
		"\"内容\"",
	}
	for _, marker := range markers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func parseSkillFieldLine(line string) (section string, value string, ok bool) {
	normalized := sanitizeSkillFieldLine(line)
	if normalized == "" {
		return "", "", false
	}

	labelPart, valuePart, ok := strings.Cut(normalized, ":")
	if !ok {
		labelPart, valuePart, ok = strings.Cut(normalized, "：")
	}
	if !ok {
		return "", "", false
	}

	label := canonicalizeSkillLabel(labelPart)
	rawValue := strings.TrimSpace(valuePart)
	value = cleanInlineMarkdown(rawValue)
	if value == "" && strings.Trim(rawValue, "*_` ") != "" {
		value = rawValue
	}

	switch label {
	case "技能名称", "技能名", "技能", "名称", "name", "skill", "skillname", "title", "标题":
		return "name", value, true
	case "描述", "description", "desc", "summary":
		return "description", value, true
	case "内容", "content", "instruction", "instructions", "body":
		return "content", value, true
	case "标签", "tag", "tags", "labels":
		return "tags", value, true
	default:
		return "", "", false
	}
}

func parseSkillSectionHeading(line string) (string, bool) {
	label := canonicalizeSkillLabel(sanitizeSkillFieldLine(line))
	switch label {
	case "技能名称", "技能名", "技能", "名称", "name", "skill", "skillname", "title", "标题":
		return "name", true
	case "描述", "description", "desc", "summary":
		return "description", true
	case "内容", "content", "instruction", "instructions", "body":
		return "content", true
	case "标签", "tag", "tags", "labels":
		return "tags", true
	default:
		return "", false
	}
}

func sanitizeSkillFieldLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || isCodeFenceLine(line) {
		return ""
	}

	line = strings.TrimLeft(line, "#")
	line = strings.TrimSpace(line)

	for {
		trimmed := line
		for _, prefix := range []string{"- ", "* ", "+ ", "• "} {
			if strings.HasPrefix(trimmed, prefix) {
				trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
				break
			}
		}
		if trimmed == line {
			break
		}
		line = trimmed
	}

	return strings.TrimSpace(line)
}

func canonicalizeSkillLabel(label string) string {
	label = cleanInlineMarkdown(label)
	label = strings.ToLower(label)
	replacer := strings.NewReplacer(" ", "", "\t", "", "：", "", ":", "")
	return replacer.Replace(label)
}

func cleanInlineMarkdown(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return strings.Trim(strings.TrimSpace(text), "*_` ")
}

func splitTags(tagStr string) []string {
	tagStr = strings.ReplaceAll(tagStr, "，", ",")
	tagStr = strings.ReplaceAll(tagStr, "、", ",")
	parts := strings.Split(tagStr, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		tag := cleanInlineMarkdown(part)
		if tag == "" {
			continue
		}
		tags = append(tags, tag)
	}
	return tags
}

func isCodeFenceLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

func firstNonEmptyString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if s := cleanInlineMarkdown(fmt.Sprint(value)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func extractStringSlice(raw map[string]any, keys ...string) []string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}

		switch v := value.(type) {
		case []any:
			items := make([]string, 0, len(v))
			for _, item := range v {
				tag := cleanInlineMarkdown(fmt.Sprint(item))
				if tag == "" || tag == "<nil>" {
					continue
				}
				items = append(items, tag)
			}
			if len(items) > 0 {
				return items
			}
		case []string:
			return splitTags(strings.Join(v, ","))
		case string:
			tags := splitTags(v)
			if len(tags) > 0 {
				return tags
			}
		}
	}
	return []string{}
}

func previewSkillResponse(response string, limit int) string {
	response = normalizeSkillResponse(response)
	runes := []rune(response)
	if len(runes) <= limit {
		return response
	}
	return string(runes[:limit]) + "...(truncated)"
}

func isSoftSkillParseError(err error) bool {
	if err == nil {
		return false
	}

	message := err.Error()
	return strings.Contains(message, "技能名称为空") ||
		strings.Contains(message, "技能内容为空") ||
		strings.Contains(message, "技能名称或内容为空")
}

// buildSkillReviewPrompt 构建技能审查提示词
// 对应 Hermes-Agent 的 _SKILL_REVIEW_PROMPT
func buildSkillReviewPrompt() string {
	return `请分析上述对话，判断是否包含值得保存的可重用技能。

重点关注以下方面：
1. 是否使用了非常规的方法来完成任务？
2. 是否经历了试错过程或根据经验调整方法？
3. 是否包含了用户期望的特定方法或结果？
4. 这种方法是否可以在类似场景中重复使用？

如果发现相关的技能已存在，请更新现有技能。
如果发现新的可重用方法，请创建新技能。

请按以下格式输出技能（如果值得保存）：

技能名称: [简短的技能名称]
描述: [技能的简要描述]
标签: [相关标签，用逗号分隔]
内容: [详细的技能指令和步骤]

如果没有值得保存的技能，请回复 "无技能值得保存"`
}
