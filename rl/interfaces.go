// Package rl 提供强化学习集成核心接口
// 遵循 Google Go 编程规范，实现轨迹压缩和策略优化功能
package rl

import (
	"context"
	"time"
)

// TrajectoryTracker 定义轨迹跟踪的核心接口
// 单一职责：记录和管理 Agent 执行轨迹
type TrajectoryTracker interface {
	// StartTracking 开始跟踪指定会话的轨迹
	StartTracking(ctx context.Context, sessionID string, agentID string) error

	// RecordStep 记录一个执行步骤
	RecordStep(ctx context.Context, sessionID string, step *TrajectoryStep) error

	// FinishTracking 结束轨迹跟踪并返回完整轨迹
	FinishTracking(ctx context.Context, sessionID string) (*Trajectory, error)

	// GetTrajectory 获取指定会话的轨迹
	GetTrajectory(ctx context.Context, sessionID string) (*Trajectory, error)

	// ListTrajectories 列出所有轨迹
	ListTrajectories(ctx context.Context, agentID string, limit int) ([]*TrajectoryMetadata, error)
}

// TrajectoryCompressor 定义轨迹压缩的核心接口
// 单一职责：将长轨迹压缩为适合训练的格式
type TrajectoryCompressor interface {
	// CompressTrajectory 压缩轨迹，保护重要部分
	CompressTrajectory(ctx context.Context, trajectory *Trajectory, config *CompressionConfig) (*CompressedTrajectory, error)

	// ShouldCompress 判断轨迹是否需要压缩
	ShouldCompress(trajectory *Trajectory, config *CompressionConfig) bool

	// GetCompressionStats 获取压缩统计信息
	GetCompressionStats() *CompressionStats
}

// PolicyOptimizer 定义策略优化的核心接口
// 单一职责：基于轨迹数据优化 Agent 决策策略
type PolicyOptimizer interface {
	// OptimizeRouterPolicy 优化路由策略
	OptimizeRouterPolicy(ctx context.Context, trajectories []*Trajectory) (*RouterPolicy, error)

	// OptimizeToolSelection 优化工具选择策略
	OptimizeToolSelection(ctx context.Context, agentID string, trajectories []*Trajectory) (*ToolSelectionPolicy, error)

	// UpdatePolicy 更新策略
	UpdatePolicy(ctx context.Context, policy Policy) error

	// GetCurrentPolicy 获取当前策略
	GetCurrentPolicy(ctx context.Context, agentID string) (Policy, error)
}

// RewardCalculator 定义奖励计算的核心接口
// 单一职责：计算轨迹的奖励值
type RewardCalculator interface {
	// CalculateReward 计算轨迹的总奖励
	CalculateReward(ctx context.Context, trajectory *Trajectory) (float64, error)

	// CalculateStepReward 计算单步奖励
	CalculateStepReward(ctx context.Context, step *TrajectoryStep, context *StepContext) (float64, error)

	// SetRewardConfig 设置奖励配置
	SetRewardConfig(config *RewardConfig)
}

// TrajectoryStep 表示轨迹中的一个步骤
type TrajectoryStep struct {
	StepID       string                 `json:"step_id"`       // 步骤唯一标识
	Timestamp    time.Time              `json:"timestamp"`     // 时间戳
	Input        string                 `json:"input"`         // 输入消息
	Thought      string                 `json:"thought"`       // Agent 思考过程
	Action       string                 `json:"action"`        // 执行的动作
	ActionInput  map[string]any         `json:"action_input"`  // 动作参数
	Observation  string                 `json:"observation"`   // 观察结果
	Success      bool                   `json:"success"`       // 是否成功
	Duration     time.Duration          `json:"duration"`      // 执行时长
	TokensUsed   int                    `json:"tokens_used"`   // 使用的 token 数量
	Metadata     map[string]any         `json:"metadata"`      // 附加元数据
}

// Trajectory 表示完整的执行轨迹
type Trajectory struct {
	ID          string            `json:"id"`           // 轨迹唯一标识
	SessionID   string            `json:"session_id"`   // 会话ID
	AgentID     string            `json:"agent_id"`     // Agent ID
	StartTime   time.Time         `json:"start_time"`   // 开始时间
	EndTime     time.Time         `json:"end_time"`     // 结束时间
	Steps       []*TrajectoryStep `json:"steps"`        // 执行步骤
	TotalReward float64           `json:"total_reward"` // 总奖励
	Success     bool              `json:"success"`      // 整体是否成功
	TokensUsed  int               `json:"tokens_used"`  // 总token使用量
	Metadata    map[string]any    `json:"metadata"`     // 轨迹元数据
}

// CompressedTrajectory 压缩后的轨迹
type CompressedTrajectory struct {
	*Trajectory                           // 继承原轨迹
	CompressedSteps   []*TrajectoryStep   `json:"compressed_steps"`   // 压缩后的步骤
	ProtectedRegions  []ProtectedRegion   `json:"protected_regions"`  // 保护区域
	CompressionRatio  float64             `json:"compression_ratio"`  // 压缩比率
	CompressionTime   time.Time           `json:"compression_time"`   // 压缩时间
}

// ProtectedRegion 保护区域定义
type ProtectedRegion struct {
	StartIndex int    `json:"start_index"` // 开始索引
	EndIndex   int    `json:"end_index"`   // 结束索引
	Reason     string `json:"reason"`      // 保护原因
}

// TrajectoryMetadata 轨迹元数据
type TrajectoryMetadata struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	AgentID   string    `json:"agent_id"`
	StartTime time.Time `json:"start_time"`
	Duration  time.Duration `json:"duration"`
	StepCount int       `json:"step_count"`
	Success   bool      `json:"success"`
	Reward    float64   `json:"reward"`
}

// CompressionConfig 压缩配置
type CompressionConfig struct {
	TargetMaxSteps       int     `yaml:"target_max_steps"`        // 目标最大步骤数
	SummaryTargetLength  int     `yaml:"summary_target_length"`   // 摘要目标长度

	// 保护策略
	ProtectedSteps struct {
		FirstN       int  `yaml:"first_n"`        // 保护前N步
		LastN        int  `yaml:"last_n"`         // 保护后N步
		ProtectError bool `yaml:"protect_error"`  // 保护错误步骤
		ProtectTool  bool `yaml:"protect_tool"`   // 保护工具调用
	} `yaml:"protected_steps"`

	// 压缩策略
	CompressionStrategy string  `yaml:"compression_strategy"` // summarize, merge, skip
	ModelName          string  `yaml:"model_name"`           // 用于压缩的模型
	MinCompressionRatio float64 `yaml:"min_compression_ratio"` // 最小压缩比率
}

// CompressionStats 压缩统计信息
type CompressionStats struct {
	TotalCompressed      int           `json:"total_compressed"`
	AverageCompressionRatio float64    `json:"average_compression_ratio"`
	AverageCompressionTime  time.Duration `json:"average_compression_time"`
	TokensSaved          int           `json:"tokens_saved"`
}

// RouterPolicy 路由策略
type RouterPolicy struct {
	LayerWeights  map[string]float64 `json:"layer_weights"`  // 各层权重
	AgentScores   map[string]float64 `json:"agent_scores"`   // Agent 效果评分
	ContextRules  []ContextualRule   `json:"context_rules"`  // 上下文相关规则
	UpdateTime    time.Time          `json:"update_time"`    // 更新时间
}

// ToolSelectionPolicy 工具选择策略
type ToolSelectionPolicy struct {
	AgentID       string             `json:"agent_id"`       // Agent ID
	ToolScores    map[string]float64 `json:"tool_scores"`    // 工具评分
	ToolPairs     map[string]float64 `json:"tool_pairs"`     // 工具组合评分
	ContextRules  []ToolContextRule  `json:"context_rules"`  // 上下文规则
	UpdateTime    time.Time          `json:"update_time"`    // 更新时间
}

// ContextualRule 上下文规则
type ContextualRule struct {
	Pattern     string             `json:"pattern"`     // 匹配模式
	AgentID     string             `json:"agent_id"`    // 目标Agent
	Confidence  float64            `json:"confidence"`  // 置信度
	Conditions  map[string]any     `json:"conditions"`  // 触发条件
}

// ToolContextRule 工具上下文规则
type ToolContextRule struct {
	Pattern     string             `json:"pattern"`     // 匹配模式
	PreferTools []string           `json:"prefer_tools"` // 偏好工具
	AvoidTools  []string           `json:"avoid_tools"`  // 避免工具
	Confidence  float64            `json:"confidence"`  // 置信度
}

// Policy 策略接口
type Policy interface {
	GetType() string
	GetAgentID() string
	GetUpdateTime() time.Time
	Validate() error
}

// StepContext 步骤上下文
type StepContext struct {
	PreviousSteps []*TrajectoryStep  `json:"previous_steps"` // 之前的步骤
	AgentState    map[string]any     `json:"agent_state"`    // Agent 状态
	Environment   map[string]any     `json:"environment"`    // 环境状态
}

// RewardConfig 奖励配置
type RewardConfig struct {
	// 成功奖励
	SuccessReward float64 `yaml:"success_reward"` // 成功完成奖励

	// 时间相关
	TimeReward struct {
		MaxDuration    time.Duration `yaml:"max_duration"`     // 最大期望时长
		PenaltyPerStep float64       `yaml:"penalty_per_step"` // 每步惩罚
	} `yaml:"time_reward"`

	// Token 相关
	TokenReward struct {
		MaxTokens       int     `yaml:"max_tokens"`        // 最大期望token
		PenaltyPerToken float64 `yaml:"penalty_per_token"` // 每token惩罚
	} `yaml:"token_reward"`

	// 工具使用
	ToolReward struct {
		SuccessBonus float64 `yaml:"success_bonus"` // 工具成功奖励
		FailurePenalty float64 `yaml:"failure_penalty"` // 工具失败惩罚
	} `yaml:"tool_reward"`

	// 错误处理
	ErrorReward struct {
		RecoveryBonus  float64 `yaml:"recovery_bonus"`  // 错误恢复奖励
		RepeatedPenalty float64 `yaml:"repeated_penalty"` // 重复错误惩罚
	} `yaml:"error_reward"`
}

// RLConfig 强化学习总配置
type RLConfig struct {
	Enabled bool `yaml:"enabled"` // 是否启用RL功能

	// 轨迹跟踪配置
	Tracking struct {
		StoragePath    string        `yaml:"storage_path"`    // 轨迹存储路径
		MaxTrajectories int          `yaml:"max_trajectories"` // 最大轨迹数量
		RetentionDays  int           `yaml:"retention_days"`  // 保留天数
		AutoCleanup    bool          `yaml:"auto_cleanup"`    // 自动清理
	} `yaml:"tracking"`

	// 压缩配置
	Compression CompressionConfig `yaml:"compression"`

	// 奖励配置
	Reward RewardConfig `yaml:"reward"`

	// 策略优化配置
	Optimization struct {
		UpdateInterval    time.Duration `yaml:"update_interval"`    // 策略更新间隔
		MinTrajectories   int           `yaml:"min_trajectories"`   // 最小轨迹数量
		LearningRate      float64       `yaml:"learning_rate"`      // 学习率
		ExplorationRate   float64       `yaml:"exploration_rate"`   // 探索率
	} `yaml:"optimization"`
}