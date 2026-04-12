// Package learning 提供自学习循环和技能管理核心接口
// 遵循 Google Go 编程规范，采用接口优先的设计原则
package learning

import (
	"context"
	"time"
)

// SkillExtractor 定义技能提取的核心接口
// 单一职责：从会话历史中识别并提取可重用的技能
type SkillExtractor interface {
	// ExtractSkill 从对话历史中提取技能
	// 返回 nil 表示没有发现值得保存的技能
	ExtractSkill(ctx context.Context, history []ConversationTurn) (*Skill, error)

	// ShouldExtract 判断当前会话是否值得进行技能提取
	ShouldExtract(history []ConversationTurn, toolUsageCount int) bool
}

// SkillRepository 定义技能存储和检索的核心接口
// 单一职责：管理技能的持久化存储和版本控制
type SkillRepository interface {
	// SaveSkill 保存或更新技能
	SaveSkill(ctx context.Context, skill *Skill) error

	// GetSkill 根据名称获取技能
	GetSkill(ctx context.Context, name string) (*Skill, error)

	// ListSkills 列出所有技能
	ListSkills(ctx context.Context) ([]*SkillMetadata, error)

	// DeleteSkill 删除技能
	DeleteSkill(ctx context.Context, name string) error

	// FindSimilar 查找相似技能（基于描述或内容）
	FindSimilar(ctx context.Context, query string, limit int) ([]*SkillMetadata, error)
}

// LearningEngine 定义自学习引擎的核心接口
// 单一职责：协调整个学习过程，包括触发、提取、存储
type LearningEngine interface {
	// StartLearning 启动学习引擎
	StartLearning(ctx context.Context) error

	// StopLearning 停止学习引擎
	StopLearning() error

	// ProcessConversation 处理单次会话，可能触发技能提取
	ProcessConversation(ctx context.Context, agentID string, conversation *Conversation) error

	// GetLearningStats 获取学习统计信息
	GetLearningStats() *LearningStats
}

// SkillInjector 定义技能注入的核心接口
// 单一职责：将相关技能注入到 Agent 的系统提示中
type SkillInjector interface {
	// InjectSkills 为指定 Agent 注入相关技能
	InjectSkills(ctx context.Context, agentID string, userQuery string) ([]string, error)

	// GetRelevantSkills 获取与查询相关的技能
	GetRelevantSkills(ctx context.Context, query string, maxSkills int) ([]*Skill, error)
}

// ConversationTurn 表示一个对话回合
type ConversationTurn struct {
	Role       string            `json:"role"`        // system, user, assistant
	Content    string            `json:"content"`     // 消息内容
	ToolCalls  []ToolCall        `json:"tool_calls"`  // 工具调用记录
	Timestamp  time.Time         `json:"timestamp"`   // 时间戳
	Metadata   map[string]any    `json:"metadata"`    // 额外元数据
}

// ToolCall 表示一次工具调用
type ToolCall struct {
	ID       string         `json:"id"`        // 工具调用ID
	Name     string         `json:"name"`      // 工具名称
	Args     map[string]any `json:"args"`      // 工具参数
	Result   string         `json:"result"`    // 工具结果
	Duration time.Duration  `json:"duration"`  // 执行时长
	Success  bool           `json:"success"`   // 是否成功
}

// Conversation 表示完整的会话记录
type Conversation struct {
	SessionID string              `json:"session_id"` // 会话ID
	AgentID   string              `json:"agent_id"`   // Agent ID
	Turns     []ConversationTurn  `json:"turns"`      // 对话回合
	StartTime time.Time           `json:"start_time"` // 开始时间
	EndTime   time.Time           `json:"end_time"`   // 结束时间
	Metadata  map[string]any      `json:"metadata"`   // 会话元数据
}

// Skill 表示一个学习到的技能
type Skill struct {
	Name        string                 `json:"name" yaml:"name"`               // 技能名称（唯一标识）
	Description string                 `json:"description" yaml:"description"` // 技能描述
	Content     string                 `json:"content" yaml:"content"`         // 技能内容（指令）
	Version     string                 `json:"version" yaml:"version"`         // 版本号
	Tags        []string               `json:"tags" yaml:"tags"`               // 标签
	CreatedAt   time.Time              `json:"created_at" yaml:"created_at"`   // 创建时间
	UpdatedAt   time.Time              `json:"updated_at" yaml:"updated_at"`   // 更新时间
	UsageCount  int                    `json:"usage_count"`                    // 使用次数
	SuccessRate float64                `json:"success_rate"`                   // 成功率
	Metadata    map[string]any         `json:"metadata" yaml:"metadata"`       // 附加元数据

	// 学习来源信息
	LearnedFrom struct {
		SessionID string    `json:"session_id"` // 来源会话
		AgentID   string    `json:"agent_id"`   // 来源Agent
		Timestamp time.Time `json:"timestamp"`  // 学习时间
	} `json:"learned_from"`
}

// SkillMetadata 技能元数据（用于列表展示）
type SkillMetadata struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
	UsageCount  int       `json:"usage_count"`
	SuccessRate float64   `json:"success_rate"`
}

// LearningStats 学习统计信息
type LearningStats struct {
	TotalConversationsProcessed int       `json:"total_conversations_processed"`
	SkillsExtracted            int       `json:"skills_extracted"`
	SkillsUpdated              int       `json:"skills_updated"`
	AverageExtractionTime      time.Duration `json:"average_extraction_time"`
	LastLearningTime           time.Time     `json:"last_learning_time"`

	// 按 Agent 分组的统计
	AgentStats map[string]*AgentLearningStats `json:"agent_stats"`
}

// AgentLearningStats 单个 Agent 的学习统计
type AgentLearningStats struct {
	ConversationsProcessed int       `json:"conversations_processed"`
	SkillsLearned         int       `json:"skills_learned"`
	LastActivity          time.Time `json:"last_activity"`
	TopSkills             []string  `json:"top_skills"` // 最常用技能
}

// LearningConfig 学习系统配置
type LearningConfig struct {
	Enabled bool `yaml:"enabled"` // 是否启用学习功能

	// 技能提取配置
	SkillExtraction struct {
		ReviewInterval    int    `yaml:"review_interval"`     // 检查间隔（工具调用次数）
		MinConversationLength int `yaml:"min_conversation_length"` // 最小会话长度
		ExtractorModel   string `yaml:"extractor_model"`     // 用于提取的模型
		ReviewPrompt     string `yaml:"review_prompt"`       // 审查提示词
	} `yaml:"skill_extraction"`

	// 技能存储配置
	Storage struct {
		Type     string `yaml:"type"`      // 存储类型: file, sqlite, etc.
		Path     string `yaml:"path"`      // 存储路径
		MaxSkills int   `yaml:"max_skills"` // 最大技能数量
	} `yaml:"storage"`

	// 技能注入配置
	Injection struct {
		MaxInjectSkills int     `yaml:"max_inject_skills"` // 最大注入技能数
		SimilarityThreshold float64 `yaml:"similarity_threshold"` // 相似度阈值
	} `yaml:"injection"`
}

// MessageBusExtensions 为了与现有的 bus 系统集成而定义的扩展
type MessageBusExtensions interface {
	// PublishLearningEvent 发布学习事件
	PublishLearningEvent(event *LearningEvent) error

	// SubscribeLearningEvents 订阅学习事件
	SubscribeLearningEvents(ctx context.Context, handler func(*LearningEvent)) error
}

// LearningEvent 学习事件
type LearningEvent struct {
	Type      string         `json:"type"`       // 事件类型: skill_extracted, skill_updated, etc.
	AgentID   string         `json:"agent_id"`   // 相关Agent
	SkillName string         `json:"skill_name"` // 技能名称
	Timestamp time.Time      `json:"timestamp"`  // 事件时间
	Data      map[string]any `json:"data"`       // 附加数据
}