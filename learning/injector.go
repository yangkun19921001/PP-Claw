// Package learning - 技能注入器的具体实现
// 遵循 Google Go 编程规范，实现 SkillInjector 接口
package learning

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// DefaultSkillInjector 默认技能注入器实现
// 基于相关性匹配将技能注入到 Agent 上下文中
type DefaultSkillInjector struct {
	repository SkillRepository // 技能存储库
	logger     *zap.Logger     // 日志器
	config     *SkillInjectionConfig // 配置

	// 缓存相关
	skillsCache     []*Skill   // 技能缓存
	cacheTimestamp  int64      // 缓存时间戳
	cacheMu         sync.RWMutex // 缓存锁
}

// SkillInjectionConfig 技能注入配置
type SkillInjectionConfig struct {
	MaxInjectSkills      int     // 最大注入技能数
	SimilarityThreshold  float64 // 相似度阈值
	CacheExpireSeconds   int64   // 缓存过期时间（秒）
	EnableSemanticMatch  bool    // 是否启用语义匹配
}

// SkillRelevance 技能相关性
type SkillRelevance struct {
	Skill  *Skill  // 技能
	Score  float64 // 相关性分数
	Reason string  // 匹配原因
}

// NewDefaultSkillInjector 创建默认技能注入器
func NewDefaultSkillInjector(
	repository SkillRepository,
	logger *zap.Logger,
	config *SkillInjectionConfig,
) *DefaultSkillInjector {
	if logger == nil {
		logger = zap.NewNop()
	}

	if config == nil {
		config = &SkillInjectionConfig{
			MaxInjectSkills:     3,    // 最多注入3个技能
			SimilarityThreshold: 0.3,  // 相似度阈值30%
			CacheExpireSeconds:  300,  // 缓存5分钟
			EnableSemanticMatch: false, // 暂不启用语义匹配
		}
	}

	return &DefaultSkillInjector{
		repository: repository,
		logger:     logger,
		config:     config,
	}
}

// InjectSkills 实现 SkillInjector.InjectSkills
// 为指定 Agent 注入相关技能
func (i *DefaultSkillInjector) InjectSkills(
	ctx context.Context,
	agentID string,
	userQuery string,
) ([]string, error) {
	// 获取相关技能
	relevantSkills, err := i.GetRelevantSkills(ctx, userQuery, i.config.MaxInjectSkills)
	if err != nil {
		return nil, fmt.Errorf("获取相关技能失败: %w", err)
	}

	if len(relevantSkills) == 0 {
		i.logger.Debug("未找到相关技能",
			zap.String("agent_id", agentID),
			zap.String("query", userQuery),
		)
		return []string{}, nil
	}

	// 将技能转换为注入文本
	injectedTexts := make([]string, 0, len(relevantSkills))
	for _, skill := range relevantSkills {
		injectedText := i.formatSkillForInjection(skill)
		injectedTexts = append(injectedTexts, injectedText)
	}

	i.logger.Info("技能注入成功",
		zap.String("agent_id", agentID),
		zap.String("query", userQuery),
		zap.Int("injected_skills", len(injectedTexts)),
		zap.Strings("skill_names", i.getSkillNames(relevantSkills)),
	)

	return injectedTexts, nil
}

// GetRelevantSkills 实现 SkillInjector.GetRelevantSkills
// 获取与查询相关的技能
func (i *DefaultSkillInjector) GetRelevantSkills(
	ctx context.Context,
	query string,
	maxSkills int,
) ([]*Skill, error) {
	if query == "" {
		return []*Skill{}, nil
	}

	// 获取所有技能（使用缓存）
	skills, err := i.getAllSkillsCached(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取技能列表失败: %w", err)
	}

	// 计算相关性
	relevanceList := make([]SkillRelevance, 0)
	for _, skill := range skills {
		score, reason := i.calculateRelevance(query, skill)
		if score >= i.config.SimilarityThreshold {
			relevanceList = append(relevanceList, SkillRelevance{
				Skill:  skill,
				Score:  score,
				Reason: reason,
			})
		}
	}

	// 按相关性排序
	sort.Slice(relevanceList, func(i, j int) bool {
		return relevanceList[i].Score > relevanceList[j].Score
	})

	// 返回指定数量的最相关技能
	limit := maxSkills
	if limit > len(relevanceList) {
		limit = len(relevanceList)
	}

	result := make([]*Skill, limit)
	for j := 0; j < limit; j++ {
		result[j] = relevanceList[j].Skill
	}

	i.logger.Debug("相关技能检索完成",
		zap.String("query", query),
		zap.Int("total_skills", len(skills)),
		zap.Int("relevant_skills", len(relevanceList)),
		zap.Int("returned_skills", len(result)),
	)

	return result, nil
}

// getAllSkillsCached 获取所有技能（带缓存）
func (i *DefaultSkillInjector) getAllSkillsCached(ctx context.Context) ([]*Skill, error) {
	i.cacheMu.RLock()
	// 检查缓存是否有效
	now := getCurrentTimestamp()
	if i.skillsCache != nil && now-i.cacheTimestamp < i.config.CacheExpireSeconds {
		skills := i.skillsCache
		i.cacheMu.RUnlock()
		return skills, nil
	}
	i.cacheMu.RUnlock()

	// 获取写锁并重新检查缓存
	i.cacheMu.Lock()
	defer i.cacheMu.Unlock()

	// 双重检查锁定
	if i.skillsCache != nil && getCurrentTimestamp()-i.cacheTimestamp < i.config.CacheExpireSeconds {
		return i.skillsCache, nil
	}

	// 从存储库获取技能元数据
	metadataList, err := i.repository.ListSkills(ctx)
	if err != nil {
		return nil, err
	}

	// 加载完整技能数据
	skills := make([]*Skill, 0, len(metadataList))
	for _, metadata := range metadataList {
		skill, err := i.repository.GetSkill(ctx, metadata.Name)
		if err != nil {
			i.logger.Warn("加载技能失败",
				zap.String("skill_name", metadata.Name),
				zap.Error(err),
			)
			continue
		}
		skills = append(skills, skill)
	}

	// 更新缓存
	i.skillsCache = skills
	i.cacheTimestamp = getCurrentTimestamp()

	i.logger.Debug("技能缓存已更新",
		zap.Int("total_skills", len(skills)),
	)

	return skills, nil
}

// calculateRelevance 计算技能与查询的相关性
func (i *DefaultSkillInjector) calculateRelevance(query string, skill *Skill) (float64, string) {
	queryLower := strings.ToLower(query)
	var score float64
	var reasons []string

	// 1. 名称匹配 (权重: 0.4)
	nameScore := i.textSimilarity(queryLower, strings.ToLower(skill.Name))
	if nameScore > 0 {
		score += nameScore * 0.4
		reasons = append(reasons, "name_match")
	}

	// 2. 描述匹配 (权重: 0.3)
	descScore := i.textSimilarity(queryLower, strings.ToLower(skill.Description))
	if descScore > 0 {
		score += descScore * 0.3
		reasons = append(reasons, "description_match")
	}

	// 3. 标签匹配 (权重: 0.2)
	tagScore := i.calculateTagSimilarity(queryLower, skill.Tags)
	if tagScore > 0 {
		score += tagScore * 0.2
		reasons = append(reasons, "tag_match")
	}

	// 4. 内容关键词匹配 (权重: 0.1)
	contentScore := i.calculateContentSimilarity(queryLower, skill.Content)
	if contentScore > 0 {
		score += contentScore * 0.1
		reasons = append(reasons, "content_match")
	}

	// 5. 使用频率加权
	if skill.UsageCount > 0 {
		usageBonus := float64(skill.UsageCount) * 0.01 // 每次使用增加1%相关性
		if usageBonus > 0.1 {
			usageBonus = 0.1 // 最多增加10%
		}
		score += usageBonus
		reasons = append(reasons, "usage_bonus")
	}

	// 6. 成功率加权
	if skill.SuccessRate > 0 {
		score *= skill.SuccessRate // 成功率作为乘数
		reasons = append(reasons, "success_rate_weighted")
	}

	reason := strings.Join(reasons, ",")
	return score, reason
}

// textSimilarity 计算文本相似度（简单实现）
func (i *DefaultSkillInjector) textSimilarity(query, text string) float64 {
	if query == "" || text == "" {
		return 0
	}

	// 1. 精确匹配
	if strings.Contains(text, query) {
		return 1.0
	}

	// 2. 词汇匹配
	queryWords := strings.Fields(query)
	textWords := strings.Fields(text)

	matchCount := 0
	for _, qWord := range queryWords {
		for _, tWord := range textWords {
			if qWord == tWord {
				matchCount++
				break
			}
		}
	}

	if len(queryWords) == 0 {
		return 0
	}

	return float64(matchCount) / float64(len(queryWords))
}

// calculateTagSimilarity 计算标签相似度
func (i *DefaultSkillInjector) calculateTagSimilarity(query string, tags []string) float64 {
	if len(tags) == 0 {
		return 0
	}

	maxScore := 0.0
	for _, tag := range tags {
		tagLower := strings.ToLower(tag)
		score := i.textSimilarity(query, tagLower)
		if score > maxScore {
			maxScore = score
		}
	}

	return maxScore
}

// calculateContentSimilarity 计算内容相似度
func (i *DefaultSkillInjector) calculateContentSimilarity(query string, content string) float64 {
	if content == "" {
		return 0
	}

	// 简单的关键词匹配
	contentLower := strings.ToLower(content)
	if strings.Contains(contentLower, query) {
		return 0.5 // 内容匹配相关性较低
	}

	// 词汇级别匹配
	queryWords := strings.Fields(query)
	matchCount := 0
	for _, word := range queryWords {
		if strings.Contains(contentLower, word) {
			matchCount++
		}
	}

	if len(queryWords) == 0 {
		return 0
	}

	return float64(matchCount) / float64(len(queryWords)) * 0.3
}

// formatSkillForInjection 将技能格式化为注入文本
func (i *DefaultSkillInjector) formatSkillForInjection(skill *Skill) string {
	var builder strings.Builder

	builder.WriteString("=== 相关技能: ")
	builder.WriteString(skill.Name)
	builder.WriteString(" ===\n")

	if skill.Description != "" {
		builder.WriteString("描述: ")
		builder.WriteString(skill.Description)
		builder.WriteString("\n")
	}

	if len(skill.Tags) > 0 {
		builder.WriteString("标签: ")
		builder.WriteString(strings.Join(skill.Tags, ", "))
		builder.WriteString("\n")
	}

	if skill.Content != "" {
		builder.WriteString("技能内容:\n")
		builder.WriteString(skill.Content)
		builder.WriteString("\n")
	}

	if skill.UsageCount > 0 {
		builder.WriteString(fmt.Sprintf("使用次数: %d", skill.UsageCount))
		if skill.SuccessRate > 0 {
			builder.WriteString(fmt.Sprintf(" | 成功率: %.1f%%", skill.SuccessRate*100))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("=== 技能结束 ===\n")

	return builder.String()
}

// getSkillNames 获取技能名称列表
func (i *DefaultSkillInjector) getSkillNames(skills []*Skill) []string {
	names := make([]string, len(skills))
	for i, skill := range skills {
		names[i] = skill.Name
	}
	return names
}

// getCurrentTimestamp 获取当前时间戳
func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}

// InvalidateCache 使缓存失效
func (i *DefaultSkillInjector) InvalidateCache() {
	i.cacheMu.Lock()
	defer i.cacheMu.Unlock()

	i.skillsCache = nil
	i.cacheTimestamp = 0

	i.logger.Debug("技能缓存已失效")
}