// Package learning - 技能提取器的具体实现
// 遵循 Google Go 编程规范，实现 SkillExtractor 接口
package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	einomodel "github.com/cloudwego/eino/components/model"
	"go.uber.org/zap"
)

// DefaultSkillExtractor 默认技能提取器实现
// 使用 LLM 分析会话历史，识别可重用的技能模式
type DefaultSkillExtractor struct {
	model      einomodel.ToolCallingChatModel // LLM 客户端
	logger     *zap.Logger         // 日志器
	config     *SkillExtractionConfig // 配置
	reviewPrompt string            // 技能审查提示词
}

// SkillExtractionConfig 技能提取配置
type SkillExtractionConfig struct {
	ReviewInterval        int    // 触发技能审查的工具调用间隔
	MinConversationLength int    // 最小对话长度
	MinToolCalls         int    // 最小工具调用次数
	ExtractorModel       string // 提取器使用的模型
}

// NewDefaultSkillExtractor 创建默认技能提取器
func NewDefaultSkillExtractor(
	model einomodel.ToolCallingChatModel,
	logger *zap.Logger,
	config *SkillExtractionConfig,
) *DefaultSkillExtractor {
	if config == nil {
		config = &SkillExtractionConfig{
			ReviewInterval:        10,  // 每10次工具调用检查一次
			MinConversationLength: 5,   // 至少5轮对话
			MinToolCalls:         3,   // 至少3次工具调用
			ExtractorModel:       "", // 使用默认模型
		}
	}

	return &DefaultSkillExtractor{
		model:      model,
		logger:     logger,
		config:     config,
		reviewPrompt: buildSkillReviewPrompt(),
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
	// 如果明确表示没有技能
	if strings.Contains(response, "Nothing to save") ||
	   strings.Contains(response, "无技能值得保存") ||
	   strings.Contains(response, "没有技能") {
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
	return e.parseNaturalLanguageSkill(response)
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

	var skill Skill
	if err := json.Unmarshal([]byte(jsonStr), &skill); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}

	if skill.Name == "" || skill.Content == "" {
		return nil, fmt.Errorf("技能名称或内容为空")
	}

	return &skill, nil
}

// parseNaturalLanguageSkill 解析自然语言格式的技能
func (e *DefaultSkillExtractor) parseNaturalLanguageSkill(response string) (*Skill, error) {
	lines := strings.Split(response, "\n")

	skill := &Skill{
		Tags: []string{},
		Metadata: make(map[string]any),
	}

	var currentSection string
	var contentLines []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 识别各个部分
		if strings.HasPrefix(line, "技能名称:") || strings.HasPrefix(line, "Name:") {
			skill.Name = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			currentSection = "name"
		} else if strings.HasPrefix(line, "描述:") || strings.HasPrefix(line, "Description:") {
			skill.Description = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			currentSection = "description"
		} else if strings.HasPrefix(line, "内容:") || strings.HasPrefix(line, "Content:") {
			currentSection = "content"
			if len(strings.SplitN(line, ":", 2)) > 1 {
				contentLines = append(contentLines, strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))
			}
		} else if strings.HasPrefix(line, "标签:") || strings.HasPrefix(line, "Tags:") {
			tagStr := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			skill.Tags = strings.Split(tagStr, ",")
			for i := range skill.Tags {
				skill.Tags[i] = strings.TrimSpace(skill.Tags[i])
			}
			currentSection = "tags"
		} else if currentSection == "content" {
			contentLines = append(contentLines, line)
		}
	}

	skill.Content = strings.Join(contentLines, "\n")

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