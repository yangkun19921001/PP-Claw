// Package learning - 学习引擎的具体实现
// 遵循 Google Go 编程规范，实现 LearningEngine 接口
package learning

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// DefaultLearningEngine 默认学习引擎实现
// 协调整个学习过程，包括触发、提取、存储和统计
type DefaultLearningEngine struct {
	extractor  SkillExtractor  // 技能提取器
	repository SkillRepository // 技能存储库
	logger     *zap.Logger     // 日志器
	config     *LearningConfig // 配置

	// 运行状态
	running      int32              // 原子性运行状态标识
	ctx          context.Context    // 运行上下文
	cancel       context.CancelFunc // 取消函数
	wg           sync.WaitGroup     // 等待组

	// 学习队列和统计
	learningQueue chan *learningTask   // 学习任务队列
	stats         *LearningStats       // 学习统计
	statsMu       sync.RWMutex         // 统计数据锁

	// Agent 相关状态跟踪
	agentIterations sync.Map // agentID -> iteration count
}

// learningTask 学习任务
type learningTask struct {
	agentID      string        // Agent ID
	conversation *Conversation // 会话数据
	timestamp    time.Time     // 任务时间戳
}

// NewDefaultLearningEngine 创建默认学习引擎
func NewDefaultLearningEngine(
	extractor SkillExtractor,
	repository SkillRepository,
	logger *zap.Logger,
	config *LearningConfig,
) *DefaultLearningEngine {
	if logger == nil {
		logger = zap.NewNop()
	}

	if config == nil {
		config = &LearningConfig{
			Enabled: true,
			SkillExtraction: struct {
				ReviewInterval        int    `yaml:"review_interval"`
				MinConversationLength int    `yaml:"min_conversation_length"`
				ExtractorModel        string `yaml:"extractor_model"`
				ReviewPrompt          string `yaml:"review_prompt"`
			}{
				ReviewInterval:        10, // 每10次工具调用检查一次
				MinConversationLength: 5,  // 至少5轮对话
			},
		}
	}

	return &DefaultLearningEngine{
		extractor:     extractor,
		repository:    repository,
		logger:        logger,
		config:        config,
		learningQueue: make(chan *learningTask, 100), // 缓冲队列
		stats: &LearningStats{
			AgentStats: make(map[string]*AgentLearningStats),
		},
	}
}

// StartLearning 实现 LearningEngine.StartLearning
// 启动学习引擎的后台处理
func (e *DefaultLearningEngine) StartLearning(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&e.running, 0, 1) {
		return fmt.Errorf("学习引擎已经在运行")
	}

	e.ctx, e.cancel = context.WithCancel(ctx)

	// 启动学习任务处理 goroutine
	e.wg.Add(1)
	go e.processLearningTasks()

	// 启动统计更新 goroutine
	e.wg.Add(1)
	go e.updateStatsLoop()

	e.logger.Info("学习引擎已启动",
		zap.Bool("enabled", e.config.Enabled),
		zap.Int("review_interval", e.config.SkillExtraction.ReviewInterval),
		zap.Int("min_conversation_length", e.config.SkillExtraction.MinConversationLength),
	)

	return nil
}

// StopLearning 实现 LearningEngine.StopLearning
// 停止学习引擎
func (e *DefaultLearningEngine) StopLearning() error {
	if !atomic.CompareAndSwapInt32(&e.running, 1, 0) {
		return fmt.Errorf("学习引擎未在运行")
	}

	// 取消上下文
	if e.cancel != nil {
		e.cancel()
	}

	// 等待所有 goroutine 退出
	e.wg.Wait()

	// 关闭队列
	close(e.learningQueue)

	e.logger.Info("学习引擎已停止")
	return nil
}

// ProcessConversation 实现 LearningEngine.ProcessConversation
// 处理单次会话，判断是否触发技能提取
func (e *DefaultLearningEngine) ProcessConversation(
	ctx context.Context,
	agentID string,
	conversation *Conversation,
) error {
	if !e.isRunning() {
		return fmt.Errorf("学习引擎未启动")
	}

	if !e.config.Enabled {
		return nil // 学习功能未启用
	}

	// 更新 Agent 迭代计数
	e.incrementAgentIterations(agentID)

	// 检查是否应该触发学习
	if !e.shouldTriggerLearning(agentID, conversation) {
		return nil
	}

	// 异步提交学习任务
	task := &learningTask{
		agentID:      agentID,
		conversation: conversation,
		timestamp:    time.Now(),
	}

	select {
	case e.learningQueue <- task:
		e.logger.Debug("学习任务已提交",
			zap.String("agent_id", agentID),
			zap.String("session_id", conversation.SessionID),
			zap.Int("conversation_length", len(conversation.Turns)),
		)
	case <-ctx.Done():
		return ctx.Err()
	default:
		e.logger.Warn("学习队列已满，跳过任务",
			zap.String("agent_id", agentID),
			zap.String("session_id", conversation.SessionID),
		)
	}

	return nil
}

// GetLearningStats 实现 LearningEngine.GetLearningStats
// 获取学习统计信息
func (e *DefaultLearningEngine) GetLearningStats() *LearningStats {
	e.statsMu.RLock()
	defer e.statsMu.RUnlock()

	// 深拷贝统计数据
	statsCopy := &LearningStats{
		TotalConversationsProcessed: e.stats.TotalConversationsProcessed,
		SkillsExtracted:            e.stats.SkillsExtracted,
		SkillsUpdated:              e.stats.SkillsUpdated,
		AverageExtractionTime:      e.stats.AverageExtractionTime,
		LastLearningTime:           e.stats.LastLearningTime,
		AgentStats:                 make(map[string]*AgentLearningStats),
	}

	// 拷贝 Agent 统计
	for agentID, agentStat := range e.stats.AgentStats {
		statsCopy.AgentStats[agentID] = &AgentLearningStats{
			ConversationsProcessed: agentStat.ConversationsProcessed,
			SkillsLearned:         agentStat.SkillsLearned,
			LastActivity:          agentStat.LastActivity,
			TopSkills:             append([]string{}, agentStat.TopSkills...),
		}
	}

	return statsCopy
}

// processLearningTasks 处理学习任务的主循环
func (e *DefaultLearningEngine) processLearningTasks() {
	defer e.wg.Done()

	e.logger.Info("学习任务处理器已启动")

	for {
		select {
		case task := <-e.learningQueue:
			if task == nil {
				// 队列已关闭
				return
			}
			e.handleLearningTask(task)
		case <-e.ctx.Done():
			e.logger.Info("学习任务处理器退出")
			return
		}
	}
}

// handleLearningTask 处理单个学习任务
func (e *DefaultLearningEngine) handleLearningTask(task *learningTask) {
	start := time.Now()

	e.logger.Debug("开始处理学习任务",
		zap.String("agent_id", task.agentID),
		zap.String("session_id", task.conversation.SessionID),
	)

	// 提取技能
	skill, err := e.extractor.ExtractSkill(e.ctx, task.conversation.Turns)
	if err != nil {
		e.logger.Error("技能提取失败",
			zap.String("agent_id", task.agentID),
			zap.String("session_id", task.conversation.SessionID),
			zap.Error(err),
		)
		return
	}

	extractionTime := time.Since(start)

	// 更新统计
	e.updateStats(func(stats *LearningStats) {
		stats.TotalConversationsProcessed++
		stats.LastLearningTime = time.Now()

		// 更新平均提取时间
		if stats.TotalConversationsProcessed == 1 {
			stats.AverageExtractionTime = extractionTime
		} else {
			// 计算加权平均
			total := float64(stats.TotalConversationsProcessed)
			stats.AverageExtractionTime = time.Duration(
				(float64(stats.AverageExtractionTime)*(total-1) + float64(extractionTime)) / total,
			)
		}

		// 更新 Agent 统计
		agentStat, exists := stats.AgentStats[task.agentID]
		if !exists {
			agentStat = &AgentLearningStats{
				TopSkills: make([]string, 0),
			}
			stats.AgentStats[task.agentID] = agentStat
		}
		agentStat.ConversationsProcessed++
		agentStat.LastActivity = time.Now()
	})

	// 如果提取到了技能，保存它
	if skill != nil {
		// 设置学习来源信息
		skill.LearnedFrom.SessionID = task.conversation.SessionID
		skill.LearnedFrom.AgentID = task.agentID
		skill.LearnedFrom.Timestamp = task.timestamp

		if err := e.repository.SaveSkill(e.ctx, skill); err != nil {
			e.logger.Error("技能保存失败",
				zap.String("agent_id", task.agentID),
				zap.String("skill_name", skill.Name),
				zap.Error(err),
			)
			return
		}

		// 更新成功统计
		e.updateStats(func(stats *LearningStats) {
			stats.SkillsExtracted++

			agentStat := stats.AgentStats[task.agentID]
			agentStat.SkillsLearned++

			// 更新 Top Skills
			e.updateTopSkills(agentStat, skill.Name)
		})

		e.logger.Info("技能学习成功",
			zap.String("agent_id", task.agentID),
			zap.String("skill_name", skill.Name),
			zap.String("session_id", task.conversation.SessionID),
			zap.Duration("extraction_time", extractionTime),
		)
	} else {
		e.logger.Debug("未发现值得学习的技能",
			zap.String("agent_id", task.agentID),
			zap.String("session_id", task.conversation.SessionID),
		)
	}
}

// updateStatsLoop 定期更新统计信息
func (e *DefaultLearningEngine) updateStatsLoop() {
	defer e.wg.Done()

	ticker := time.NewTicker(1 * time.Minute) // 每分钟更新一次
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 这里可以添加定期统计更新逻辑
			// 例如：清理过期的 Agent 迭代计数等
		case <-e.ctx.Done():
			return
		}
	}
}

// isRunning 检查学习引擎是否在运行
func (e *DefaultLearningEngine) isRunning() bool {
	return atomic.LoadInt32(&e.running) == 1
}

// incrementAgentIterations 增加 Agent 的迭代计数
func (e *DefaultLearningEngine) incrementAgentIterations(agentID string) {
	val, _ := e.agentIterations.LoadOrStore(agentID, int32(0))
	atomic.AddInt32(val.(*int32), 1)
}

// getAgentIterations 获取 Agent 的迭代计数
func (e *DefaultLearningEngine) getAgentIterations(agentID string) int32 {
	val, exists := e.agentIterations.Load(agentID)
	if !exists {
		return 0
	}
	return atomic.LoadInt32(val.(*int32))
}

// shouldTriggerLearning 判断是否应该触发学习
func (e *DefaultLearningEngine) shouldTriggerLearning(agentID string, conversation *Conversation) bool {
	// 检查迭代计数
	iterations := e.getAgentIterations(agentID)
	if int(iterations)%e.config.SkillExtraction.ReviewInterval != 0 {
		return false
	}

	// 让提取器做最终判断
	return e.extractor.ShouldExtract(conversation.Turns, e.countToolCalls(conversation.Turns))
}

// countToolCalls 统计工具调用次数
func (e *DefaultLearningEngine) countToolCalls(turns []ConversationTurn) int {
	count := 0
	for _, turn := range turns {
		count += len(turn.ToolCalls)
	}
	return count
}

// updateStats 安全地更新统计信息
func (e *DefaultLearningEngine) updateStats(updateFunc func(*LearningStats)) {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	updateFunc(e.stats)
}

// updateTopSkills 更新 Agent 的热门技能列表
func (e *DefaultLearningEngine) updateTopSkills(agentStat *AgentLearningStats, skillName string) {
	// 简单实现：将新技能添加到列表头部
	topSkills := make([]string, 0, len(agentStat.TopSkills)+1)
	topSkills = append(topSkills, skillName)

	// 去重并保持最多5个技能
	seen := map[string]bool{skillName: true}
	for _, skill := range agentStat.TopSkills {
		if !seen[skill] && len(topSkills) < 5 {
			topSkills = append(topSkills, skill)
			seen[skill] = true
		}
	}

	agentStat.TopSkills = topSkills
}