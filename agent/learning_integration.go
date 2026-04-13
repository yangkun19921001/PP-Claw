// Package agent - 自学习循环和强化学习集成
// 遵循 Google Go 编程规范，无缝集成到现有 Agent 系统
package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/yangkun19921001/PP-Claw/config"
	"github.com/yangkun19921001/PP-Claw/learning"
	"github.com/yangkun19921001/PP-Claw/rl"
	"go.uber.org/zap"
)

// LearningIntegration 学习功能集成器
// 单一职责：协调自学习循环和RL功能与现有Agent系统的集成
type LearningIntegration struct {
	// 核心组件
	learningEngine     learning.LearningEngine    // 学习引擎
	skillInjector     learning.SkillInjector     // 技能注入器
	trajectoryTracker rl.TrajectoryTracker       // 轨迹跟踪器
	compressor        rl.TrajectoryCompressor    // 轨迹压缩器

	// 运行状态
	enabled    bool                  // 是否启用
	logger     *zap.Logger          // 日志器
	mu         sync.RWMutex         // 读写锁

	// 统计和监控
	conversationCount int64    // 处理的会话数量
	skillsInjected   int64    // 注入的技能数量
	trajectoriesTracked int64 // 跟踪的轨迹数量
}

// LearningIntegrationConfig 学习集成配置
type LearningIntegrationConfig struct {
	LearningConfig *config.LearningConfig // 学习引擎配置
	RLConfig       *config.RLConfig       // 强化学习配置
	SkillsPath     string                 // 技能存储路径
	TrajectoriesPath string               // 轨迹存储路径
}

// NewLearningIntegration 创建学习功能集成器
func NewLearningIntegration(
	env *AgentEnv,
	logger *zap.Logger,
	cfg *LearningIntegrationConfig,
) (*LearningIntegration, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	if cfg == nil {
		return nil, fmt.Errorf("配置不能为空")
	}

	lc := cfg.LearningConfig
	rc := cfg.RLConfig

	// 桥接 config -> learning 类型
	learningCfg := &learning.LearningConfig{Enabled: lc.Enabled}
	learningCfg.SkillExtraction.ReviewInterval = lc.SkillExtraction.ReviewInterval
	learningCfg.SkillExtraction.MinConversationLength = lc.SkillExtraction.MinConversationLength
	learningCfg.SkillExtraction.ExtractorModel = lc.SkillExtraction.ExtractorModel
	learningCfg.SkillExtraction.ReviewPrompt = lc.SkillExtraction.ReviewPrompt
	learningCfg.Storage.Type = lc.Storage.Type
	learningCfg.Storage.Path = lc.Storage.Path
	learningCfg.Storage.MaxSkills = lc.Storage.MaxSkills
	learningCfg.Injection.MaxInjectSkills = lc.Injection.MaxInjectSkills
	learningCfg.Injection.SimilarityThreshold = lc.Injection.SimilarityThreshold

	// 创建技能存储库
	skillRepo, err := learning.NewFileBasedSkillRepository(
		cfg.SkillsPath,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("创建技能存储库失败: %w", err)
	}

	// 创建技能提取器
	skillExtractor := learning.NewDefaultSkillExtractor(
		env.ChatModel,
		logger,
		&learning.SkillExtractionConfig{
			ReviewInterval:        lc.SkillExtraction.ReviewInterval,
			MinConversationLength: lc.SkillExtraction.MinConversationLength,
			MinToolCalls:         lc.SkillExtraction.MinToolCalls,
			ExtractorModel:       lc.SkillExtraction.ExtractorModel,
		},
	)

	// 创建学习引擎
	learningEngine := learning.NewDefaultLearningEngine(
		skillExtractor,
		skillRepo,
		logger,
		learningCfg,
	)

	// 创建技能注入器
	skillInjector := learning.NewDefaultSkillInjector(
		skillRepo,
		logger,
		&learning.SkillInjectionConfig{
			MaxInjectSkills:     lc.Injection.MaxInjectSkills,
			SimilarityThreshold: lc.Injection.SimilarityThreshold,
			CacheExpireSeconds:  lc.Injection.CacheExpireSeconds,
			EnableSemanticMatch: lc.Injection.EnableSemanticMatch,
		},
	)

	// 创建轨迹跟踪器
	trajectoryTracker, err := rl.NewFileBasedTrajectoryTracker(
		cfg.TrajectoriesPath,
		logger,
		&rl.TrackingConfig{
			StoragePath:     cfg.TrajectoriesPath,
			MaxTrajectories: rc.Tracking.MaxTrajectories,
			RetentionDays:   rc.Tracking.RetentionDays,
			AutoCleanup:     rc.Tracking.AutoCleanup,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("创建轨迹跟踪器失败: %w", err)
	}

	// 创建轨迹压缩器
	compressor := rl.NewDefaultTrajectoryCompressor(
		env.ChatModel,
		logger,
		5, // 最大并发数
	)

	integration := &LearningIntegration{
		learningEngine:    learningEngine,
		skillInjector:     skillInjector,
		trajectoryTracker: trajectoryTracker,
		compressor:        compressor,
		enabled:           lc.Enabled && rc.Enabled,
		logger:            logger,
	}

	// 启动学习引擎
	if integration.enabled {
		if err := learningEngine.StartLearning(context.Background()); err != nil {
			return nil, fmt.Errorf("启动学习引擎失败: %w", err)
		}
		logger.Info("学习功能集成已启用")
	}

	return integration, nil
}

// EnhanceSystemPrompt 增强系统提示词
// 在现有系统提示词基础上注入相关技能
func (li *LearningIntegration) EnhanceSystemPrompt(
	ctx context.Context,
	agentID string,
	originalPrompt string,
	userQuery string,
) (string, error) {
	if !li.enabled {
		return originalPrompt, nil
	}

	li.mu.RLock()
	defer li.mu.RUnlock()

	// 注入相关技能
	skillTexts, err := li.skillInjector.InjectSkills(ctx, agentID, userQuery)
	if err != nil {
		li.logger.Error("技能注入失败",
			zap.String("agent_id", agentID),
			zap.Error(err),
		)
		return originalPrompt, nil // 失败时返回原始提示词
	}

	if len(skillTexts) == 0 {
		return originalPrompt, nil
	}

	// 更新统计
	atomic.AddInt64(&li.skillsInjected, int64(len(skillTexts)))

	// 构建增强的系统提示词
	enhancedPrompt := originalPrompt + "\n\n"
	enhancedPrompt += "=== 相关技能知识库 ===\n"
	enhancedPrompt += "以下是一些可能对当前任务有帮助的已学习技能：\n\n"

	for _, skillText := range skillTexts {
		enhancedPrompt += skillText + "\n"
	}

	enhancedPrompt += "=== 技能知识库结束 ===\n"
	enhancedPrompt += "请在适当时候参考上述技能，但不要生硬地套用。根据具体情况灵活运用。\n"

	li.logger.Debug("系统提示词已增强",
		zap.String("agent_id", agentID),
		zap.Int("injected_skills", len(skillTexts)),
		zap.Int("original_length", len(originalPrompt)),
		zap.Int("enhanced_length", len(enhancedPrompt)),
	)

	return enhancedPrompt, nil
}

// BeginConversationTracking 开始会话跟踪
// 在会话开始时调用，初始化轨迹跟踪
func (li *LearningIntegration) BeginConversationTracking(
	ctx context.Context,
	sessionID string,
	agentID string,
) error {
	if !li.enabled {
		return nil
	}

	// 开始轨迹跟踪
	if err := li.trajectoryTracker.StartTracking(ctx, sessionID, agentID); err != nil {
		li.logger.Error("开始轨迹跟踪失败",
			zap.String("session_id", sessionID),
			zap.String("agent_id", agentID),
			zap.Error(err),
		)
		return err
	}

	atomic.AddInt64(&li.trajectoriesTracked, 1)

	li.logger.Debug("会话跟踪已开始",
		zap.String("session_id", sessionID),
		zap.String("agent_id", agentID),
	)

	return nil
}

// RecordExecutionStep 记录执行步骤
// 在每个工具执行后调用，记录轨迹步骤
func (li *LearningIntegration) RecordExecutionStep(
	ctx context.Context,
	sessionID string,
	step *rl.TrajectoryStep,
) error {
	if !li.enabled {
		return nil
	}

	if err := li.trajectoryTracker.RecordStep(ctx, sessionID, step); err != nil {
		li.logger.Error("记录轨迹步骤失败",
			zap.String("session_id", sessionID),
			zap.String("step_id", step.StepID),
			zap.Error(err),
		)
		return err
	}

	li.logger.Debug("轨迹步骤已记录",
		zap.String("session_id", sessionID),
		zap.String("step_id", step.StepID),
		zap.String("action", step.Action),
		zap.Bool("success", step.Success),
	)

	return nil
}

// FinishConversationTracking 完成会话跟踪
// 在会话结束时调用，完成轨迹记录并触发学习
func (li *LearningIntegration) FinishConversationTracking(
	ctx context.Context,
	sessionID string,
	agentID string,
) error {
	if !li.enabled {
		return nil
	}

	// 完成轨迹跟踪
	trajectory, err := li.trajectoryTracker.FinishTracking(ctx, sessionID)
	if err != nil {
		li.logger.Error("完成轨迹跟踪失败",
			zap.String("session_id", sessionID),
			zap.String("agent_id", agentID),
			zap.Error(err),
		)
		return err
	}

	// 转换为学习系统所需的对话格式
	conversation := li.convertTrajectoryToConversation(trajectory)

	// 触发学习过程
	if err := li.learningEngine.ProcessConversation(ctx, agentID, conversation); err != nil {
		li.logger.Error("学习处理失败",
			zap.String("session_id", sessionID),
			zap.String("agent_id", agentID),
			zap.Error(err),
		)
		// 不返回错误，因为学习失败不应影响主流程
	}

	// 更新统计
	atomic.AddInt64(&li.conversationCount, 1)

	li.logger.Info("会话跟踪已完成",
		zap.String("session_id", sessionID),
		zap.String("agent_id", agentID),
		zap.Int("step_count", len(trajectory.Steps)),
		zap.Bool("success", trajectory.Success),
		zap.Duration("duration", trajectory.EndTime.Sub(trajectory.StartTime)),
	)

	return nil
}

// convertTrajectoryToConversation 将轨迹转换为会话格式
func (li *LearningIntegration) convertTrajectoryToConversation(
	trajectory *rl.Trajectory,
) *learning.Conversation {
	turns := make([]learning.ConversationTurn, 0, len(trajectory.Steps)*2)

	for _, step := range trajectory.Steps {
		// 用户输入轮次
		if step.Input != "" {
			userTurn := learning.ConversationTurn{
				Role:      "user",
				Content:   step.Input,
				Timestamp: step.Timestamp,
				Metadata:  map[string]any{"step_id": step.StepID},
			}
			turns = append(turns, userTurn)
		}

		// Assistant 响应轮次
		var assistantContent string
		if step.Thought != "" {
			assistantContent = step.Thought + "\n"
		}
		if step.Observation != "" {
			assistantContent += step.Observation
		}

		// 工具调用信息
		var toolCalls []learning.ToolCall
		if step.Action != "" {
			toolCall := learning.ToolCall{
				ID:       step.StepID,
				Name:     step.Action,
				Args:     step.ActionInput,
				Result:   step.Observation,
				Duration: step.Duration,
				Success:  step.Success,
			}
			toolCalls = append(toolCalls, toolCall)
		}

		assistantTurn := learning.ConversationTurn{
			Role:      "assistant",
			Content:   assistantContent,
			ToolCalls: toolCalls,
			Timestamp: step.Timestamp,
			Metadata:  map[string]any{"step_id": step.StepID},
		}
		turns = append(turns, assistantTurn)
	}

	return &learning.Conversation{
		SessionID: trajectory.SessionID,
		AgentID:   trajectory.AgentID,
		Turns:     turns,
		StartTime: trajectory.StartTime,
		EndTime:   trajectory.EndTime,
		Metadata: map[string]any{
			"trajectory_id":   trajectory.ID,
			"total_reward":    trajectory.TotalReward,
			"tokens_used":     trajectory.TokensUsed,
			"overall_success": trajectory.Success,
		},
	}
}

// GetLearningStats 获取学习统计信息
func (li *LearningIntegration) GetLearningStats() *LearningStats {
	if !li.enabled {
		return nil
	}

	engineStats := li.learningEngine.GetLearningStats()
	compressionStats := li.compressor.GetCompressionStats()

	return &LearningStats{
		ConversationCount:   atomic.LoadInt64(&li.conversationCount),
		SkillsInjected:     atomic.LoadInt64(&li.skillsInjected),
		TrajectoriesTracked: atomic.LoadInt64(&li.trajectoriesTracked),
		LearningEngine:     engineStats,
		CompressionStats:   compressionStats,
	}
}

// LearningStats 学习统计信息
type LearningStats struct {
	ConversationCount    int64                     // 处理的会话数量
	SkillsInjected      int64                     // 注入的技能数量
	TrajectoriesTracked int64                     // 跟踪的轨迹数量
	LearningEngine      *learning.LearningStats   // 学习引擎统计
	CompressionStats    *rl.CompressionStats      // 压缩统计
}

// Shutdown 关闭学习功能
func (li *LearningIntegration) Shutdown() error {
	li.mu.Lock()
	defer li.mu.Unlock()

	if !li.enabled {
		return nil
	}

	// 停止学习引擎
	if err := li.learningEngine.StopLearning(); err != nil {
		li.logger.Error("停止学习引擎失败", zap.Error(err))
		return err
	}

	li.enabled = false
	li.logger.Info("学习功能已关闭")
	return nil
}

// IsEnabled 检查学习功能是否启用
func (li *LearningIntegration) IsEnabled() bool {
	li.mu.RLock()
	defer li.mu.RUnlock()
	return li.enabled
}

// Enable 启用学习功能
func (li *LearningIntegration) Enable(ctx context.Context) error {
	li.mu.Lock()
	defer li.mu.Unlock()

	if li.enabled {
		return nil
	}

	// 启动学习引擎
	if err := li.learningEngine.StartLearning(ctx); err != nil {
		return fmt.Errorf("启动学习引擎失败: %w", err)
	}

	li.enabled = true
	li.logger.Info("学习功能已启用")
	return nil
}

// Disable 禁用学习功能
func (li *LearningIntegration) Disable() error {
	li.mu.Lock()
	defer li.mu.Unlock()

	if !li.enabled {
		return nil
	}

	// 停止学习引擎
	if err := li.learningEngine.StopLearning(); err != nil {
		li.logger.Error("停止学习引擎失败", zap.Error(err))
		return err
	}

	li.enabled = false
	li.logger.Info("学习功能已禁用")
	return nil
}