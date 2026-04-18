// Package config - 学习和强化学习配置扩展
// 遵循 Google Go 编程规范，扩展现有配置系统
package config

import (
	"path/filepath"
	"time"
)

// LearningConfig 扩展 Config 结构体，增加学习相关配置
// 向后兼容，不影响现有配置文件
type LearningConfig struct {
	Enabled bool `yaml:"enabled"` // 是否启用学习功能

	// 技能提取配置
	SkillExtraction SkillExtractionConfig `yaml:"skill_extraction"`

	// 技能存储配置
	Storage SkillStorageConfig `yaml:"storage"`

	// 技能注入配置
	Injection SkillInjectionConfig `yaml:"injection"`
}

// SkillExtractionConfig 技能提取配置
type SkillExtractionConfig struct {
	ReviewInterval        int    `yaml:"review_interval"`          // 检查间隔（工具调用次数）
	MinConversationLength int    `yaml:"min_conversation_length"`  // 最小对话长度
	MinToolCalls         int    `yaml:"min_tool_calls"`           // 最小工具调用次数
	ExtractorModel       string `yaml:"extractor_model"`          // 用于提取的模型
	ReviewPrompt         string `yaml:"review_prompt"`            // 审查提示词
}

// SkillStorageConfig 技能存储配置
type SkillStorageConfig struct {
	Type      string `yaml:"type"`       // 存储类型: file, sqlite, etc.
	Path      string `yaml:"path"`       // 存储路径
	MaxSkills int    `yaml:"max_skills"` // 最大技能数量
}

// SkillInjectionConfig 技能注入配置
type SkillInjectionConfig struct {
	MaxInjectSkills      int     `yaml:"max_inject_skills"`      // 最大注入技能数
	SimilarityThreshold  float64 `yaml:"similarity_threshold"`   // 相似度阈值
	CacheExpireSeconds   int64   `yaml:"cache_expire_seconds"`   // 缓存过期时间
	EnableSemanticMatch  bool    `yaml:"enable_semantic_match"`  // 启用语义匹配
}

// RLConfig 强化学习配置
type RLConfig struct {
	Enabled bool `yaml:"enabled"` // 是否启用RL功能

	// 轨迹跟踪配置
	Tracking TrackingConfig `yaml:"tracking"`

	// 压缩配置
	Compression CompressionConfig `yaml:"compression"`

	// 奖励配置
	Reward RewardConfig `yaml:"reward"`

	// 策略优化配置
	Optimization OptimizationConfig `yaml:"optimization"`
}

// TrackingConfig 轨迹跟踪配置
type TrackingConfig struct {
	StoragePath     string        `yaml:"storage_path"`      // 轨迹存储路径
	MaxTrajectories int           `yaml:"max_trajectories"`  // 最大轨迹数量
	RetentionDays   int           `yaml:"retention_days"`    // 保留天数
	AutoCleanup     bool          `yaml:"auto_cleanup"`      // 自动清理
	FlushInterval   time.Duration `yaml:"flush_interval"`    // 刷新间隔
}

// CompressionConfig 压缩配置
type CompressionConfig struct {
	TargetMaxSteps       int     `yaml:"target_max_steps"`        // 目标最大步骤数
	SummaryTargetLength  int     `yaml:"summary_target_length"`   // 摘要目标长度
	CompressionStrategy  string  `yaml:"compression_strategy"`    // 压缩策略
	ModelName           string  `yaml:"model_name"`              // 压缩模型
	MinCompressionRatio float64 `yaml:"min_compression_ratio"`   // 最小压缩比率

	// 保护策略
	ProtectedSteps ProtectedStepsConfig `yaml:"protected_steps"`
}

// ProtectedStepsConfig 保护步骤配置
type ProtectedStepsConfig struct {
	FirstN       int  `yaml:"first_n"`        // 保护前N步
	LastN        int  `yaml:"last_n"`         // 保护后N步
	ProtectError bool `yaml:"protect_error"`  // 保护错误步骤
	ProtectTool  bool `yaml:"protect_tool"`   // 保护工具调用
}

// RewardConfig 奖励配置
type RewardConfig struct {
	SuccessReward float64           `yaml:"success_reward"` // 成功完成奖励
	TimeReward    TimeRewardConfig  `yaml:"time_reward"`    // 时间相关奖励
	TokenReward   TokenRewardConfig `yaml:"token_reward"`   // Token相关奖励
	ToolReward    ToolRewardConfig  `yaml:"tool_reward"`    // 工具使用奖励
	ErrorReward   ErrorRewardConfig `yaml:"error_reward"`   // 错误处理奖励
}

// TimeRewardConfig 时间奖励配置
type TimeRewardConfig struct {
	MaxDuration    time.Duration `yaml:"max_duration"`     // 最大期望时长
	PenaltyPerStep float64       `yaml:"penalty_per_step"` // 每步惩罚
}

// TokenRewardConfig Token奖励配置
type TokenRewardConfig struct {
	MaxTokens       int     `yaml:"max_tokens"`         // 最大期望token
	PenaltyPerToken float64 `yaml:"penalty_per_token"`  // 每token惩罚
}

// ToolRewardConfig 工具奖励配置
type ToolRewardConfig struct {
	SuccessBonus   float64 `yaml:"success_bonus"`   // 工具成功奖励
	FailurePenalty float64 `yaml:"failure_penalty"` // 工具失败惩罚
}

// ErrorRewardConfig 错误奖励配置
type ErrorRewardConfig struct {
	RecoveryBonus   float64 `yaml:"recovery_bonus"`   // 错误恢复奖励
	RepeatedPenalty float64 `yaml:"repeated_penalty"` // 重复错误惩罚
}

// OptimizationConfig 策略优化配置
type OptimizationConfig struct {
	UpdateInterval   time.Duration `yaml:"update_interval"`    // 策略更新间隔
	MinTrajectories  int           `yaml:"min_trajectories"`   // 最小轨迹数量
	LearningRate     float64       `yaml:"learning_rate"`      // 学习率
	ExplorationRate  float64       `yaml:"exploration_rate"`   // 探索率
}

// GetDefaultLearningConfig 获取默认学习配置
func GetDefaultLearningConfig() *LearningConfig {
	return &LearningConfig{
		Enabled: false, // 默认禁用，需要明确启用

		SkillExtraction: SkillExtractionConfig{
			ReviewInterval:        10, // 每10次工具调用检查一次
			MinConversationLength: 5,  // 至少5轮对话
			MinToolCalls:         3,  // 至少3次工具调用
			ExtractorModel:       "", // 使用默认模型
			ReviewPrompt:         "", // 使用内置提示词
		},

		Storage: SkillStorageConfig{
			Type:      "file",
			Path:      "./skills", // 默认技能存储路径
			MaxSkills: 1000,       // 最多存储1000个技能
		},

		Injection: SkillInjectionConfig{
			MaxInjectSkills:     3,    // 最多注入3个技能
			SimilarityThreshold: 0.3,  // 相似度阈值30%
			CacheExpireSeconds:  300,  // 缓存5分钟
			EnableSemanticMatch: false, // 暂不启用语义匹配
		},
	}
}

// GetDefaultRLConfig 获取默认强化学习配置
func GetDefaultRLConfig() *RLConfig {
	return &RLConfig{
		Enabled: false, // 默认禁用，需要明确启用

		Tracking: TrackingConfig{
			StoragePath:     "./trajectories", // 默认轨迹存储路径
			MaxTrajectories: 1000,             // 最多存储1000条轨迹
			RetentionDays:   30,               // 保留30天
			AutoCleanup:     true,             // 自动清理
			FlushInterval:   5 * time.Minute,  // 5分钟刷新一次
		},

		Compression: CompressionConfig{
			TargetMaxSteps:      50,    // 目标最大50步
			SummaryTargetLength: 500,   // 摘要目标500字符
			CompressionStrategy: "summarize", // 摘要压缩
			ModelName:          "",     // 使用默认模型
			MinCompressionRatio: 0.3,   // 最小压缩比率30%

			ProtectedSteps: ProtectedStepsConfig{
				FirstN:       5,    // 保护前5步
				LastN:        3,    // 保护后3步
				ProtectError: true, // 保护错误步骤
				ProtectTool:  true, // 保护工具调用
			},
		},

		Reward: RewardConfig{
			SuccessReward: 1.0, // 成功奖励1.0

			TimeReward: TimeRewardConfig{
				MaxDuration:    10 * time.Minute, // 最大期望10分钟
				PenaltyPerStep: 0.01,              // 每步惩罚0.01
			},

			TokenReward: TokenRewardConfig{
				MaxTokens:       10000, // 最大期望10000 token
				PenaltyPerToken: 0.0001, // 每token惩罚0.0001
			},

			ToolReward: ToolRewardConfig{
				SuccessBonus:   0.1, // 工具成功奖励0.1
				FailurePenalty: 0.2, // 工具失败惩罚0.2
			},

			ErrorReward: ErrorRewardConfig{
				RecoveryBonus:   0.3, // 错误恢复奖励0.3
				RepeatedPenalty: 0.5, // 重复错误惩罚0.5
			},
		},

		Optimization: OptimizationConfig{
			UpdateInterval:  24 * time.Hour, // 每24小时更新一次策略
			MinTrajectories: 10,             // 至少10条轨迹才开始优化
			LearningRate:    0.01,           // 学习率1%
			ExplorationRate: 0.1,            // 探索率10%
		},
	}
}

// ResolveSkillsPath 解析技能存储路径
func (c *Config) ResolveSkillsPath(agentID string) string {
	if c.Learning == nil || c.Learning.Storage.Path == "" {
		return filepath.Join("./skills", agentID)
	}

	path := ExpandHome(c.Learning.Storage.Path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(".", path)
	}

	return filepath.Join(path, agentID)
}

// ResolveTrajectoriesPath 解析轨迹存储路径
func (c *Config) ResolveTrajectoriesPath(agentID string) string {
	if c.RL == nil || c.RL.Tracking.StoragePath == "" {
		return filepath.Join("./trajectories", agentID)
	}

	path := ExpandHome(c.RL.Tracking.StoragePath)
	if !filepath.IsAbs(path) {
		path = filepath.Join(".", path)
	}

	return filepath.Join(path, agentID)
}

// IsLearningEnabled 检查学习功能是否启用
func (c *Config) IsLearningEnabled() bool {
	return c.Learning != nil && c.Learning.Enabled
}

// IsRLEnabled 检查强化学习功能是否启用
func (c *Config) IsRLEnabled() bool {
	return c.RL != nil && c.RL.Enabled
}

// IsAnyLearningEnabled 检查是否有任何学习功能启用
func (c *Config) IsAnyLearningEnabled() bool {
	return c.IsLearningEnabled() || c.IsRLEnabled()
}

// GetLearningConfig 获取学习配置，如果为空则返回默认值
func (c *Config) GetLearningConfig() *LearningConfig {
	if c.Learning == nil {
		return GetDefaultLearningConfig()
	}
	return c.Learning
}

// GetRLConfig 获取强化学习配置，如果为空则返回默认值
func (c *Config) GetRLConfig() *RLConfig {
	if c.RL == nil {
		return GetDefaultRLConfig()
	}
	return c.RL
}