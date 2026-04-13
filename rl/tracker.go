// Package rl - 轨迹跟踪器的具体实现
// 遵循 Google Go 编程规范，实现 TrajectoryTracker 接口
package rl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// FileBasedTrajectoryTracker 基于文件系统的轨迹跟踪器实现
// 采用文件系统存储，支持并发安全的轨迹管理
type FileBasedTrajectoryTracker struct {
	basePath        string                      // 轨迹存储基础路径
	logger          *zap.Logger                 // 日志器
	mu              sync.RWMutex                // 读写锁
	activeTrajectories map[string]*Trajectory   // 活跃轨迹缓存
	config          *TrackingConfig             // 跟踪配置
}

// TrackingConfig 轨迹跟踪配置
type TrackingConfig struct {
	StoragePath     string        // 存储路径
	MaxTrajectories int           // 最大轨迹数量
	RetentionDays   int           // 保留天数
	AutoCleanup     bool          // 自动清理
	FlushInterval   time.Duration // 刷新间隔
}

// NewFileBasedTrajectoryTracker 创建文件系统轨迹跟踪器
func NewFileBasedTrajectoryTracker(
	basePath string,
	logger *zap.Logger,
	config *TrackingConfig,
) (*FileBasedTrajectoryTracker, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	if config == nil {
		config = &TrackingConfig{
			StoragePath:     basePath,
			MaxTrajectories: 1000,
			RetentionDays:   30,
			AutoCleanup:     true,
			FlushInterval:   5 * time.Minute,
		}
	}

	// 🛡️ 安全检查：确保关键字段不为零值
	if config.FlushInterval <= 0 {
		logger.Warn("FlushInterval 无效，使用默认值",
			zap.Duration("invalid_value", config.FlushInterval),
			zap.Duration("default_value", 5*time.Minute))
		config.FlushInterval = 5 * time.Minute
	}
	if config.MaxTrajectories <= 0 {
		config.MaxTrajectories = 1000
	}
	if config.RetentionDays <= 0 {
		config.RetentionDays = 30
	}

	// 确保目录存在
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("创建轨迹存储目录失败: %w", err)
	}

	tracker := &FileBasedTrajectoryTracker{
		basePath:           basePath,
		logger:             logger,
		activeTrajectories: make(map[string]*Trajectory),
		config:             config,
	}

	// 启动定期清理
	if config.AutoCleanup {
		go tracker.cleanupLoop()
	}

	return tracker, nil
}

// StartTracking 实现 TrajectoryTracker.StartTracking
// 开始跟踪指定会话的轨迹
func (t *FileBasedTrajectoryTracker) StartTracking(
	ctx context.Context,
	sessionID string,
	agentID string,
) error {
	if sessionID == "" {
		return fmt.Errorf("会话ID不能为空")
	}
	if agentID == "" {
		return fmt.Errorf("Agent ID不能为空")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// 检查是否已经在跟踪
	if _, exists := t.activeTrajectories[sessionID]; exists {
		t.logger.Warn("轨迹已在跟踪中",
			zap.String("session_id", sessionID),
			zap.String("agent_id", agentID),
		)
		return nil
	}

	// 创建新轨迹
	trajectory := &Trajectory{
		ID:        generateTrajectoryID(sessionID, agentID),
		SessionID: sessionID,
		AgentID:   agentID,
		StartTime: time.Now(),
		Steps:     make([]*TrajectoryStep, 0),
		Metadata:  make(map[string]any),
	}

	t.activeTrajectories[sessionID] = trajectory

	t.logger.Info("开始轨迹跟踪",
		zap.String("trajectory_id", trajectory.ID),
		zap.String("session_id", sessionID),
		zap.String("agent_id", agentID),
	)

	return nil
}

// RecordStep 实现 TrajectoryTracker.RecordStep
// 记录一个执行步骤
func (t *FileBasedTrajectoryTracker) RecordStep(
	ctx context.Context,
	sessionID string,
	step *TrajectoryStep,
) error {
	if sessionID == "" {
		return fmt.Errorf("会话ID不能为空")
	}
	if step == nil {
		return fmt.Errorf("步骤不能为空")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// 查找活跃轨迹
	trajectory, exists := t.activeTrajectories[sessionID]
	if !exists {
		return fmt.Errorf("会话 %s 的轨迹不存在", sessionID)
	}

	// 设置步骤ID
	if step.StepID == "" {
		step.StepID = fmt.Sprintf("step_%d", len(trajectory.Steps))
	}

	// 设置时间戳
	if step.Timestamp.IsZero() {
		step.Timestamp = time.Now()
	}

	// 添加到轨迹
	trajectory.Steps = append(trajectory.Steps, step)
	trajectory.TokensUsed += step.TokensUsed

	t.logger.Debug("记录轨迹步骤",
		zap.String("trajectory_id", trajectory.ID),
		zap.String("step_id", step.StepID),
		zap.String("action", step.Action),
		zap.Bool("success", step.Success),
	)

	return nil
}

// FinishTracking 实现 TrajectoryTracker.FinishTracking
// 结束轨迹跟踪并返回完整轨迹
func (t *FileBasedTrajectoryTracker) FinishTracking(
	ctx context.Context,
	sessionID string,
) (*Trajectory, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("会话ID不能为空")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// 查找活跃轨迹
	trajectory, exists := t.activeTrajectories[sessionID]
	if !exists {
		return nil, fmt.Errorf("会话 %s 的轨迹不存在", sessionID)
	}

	// 完成轨迹
	trajectory.EndTime = time.Now()
	trajectory.Success = t.calculateOverallSuccess(trajectory)

	// 保存到文件
	if err := t.saveTrajectoryToFile(trajectory); err != nil {
		t.logger.Error("保存轨迹失败",
			zap.String("trajectory_id", trajectory.ID),
			zap.Error(err),
		)
		// 不返回错误，轨迹仍然可用
	}

	// 从活跃列表移除
	delete(t.activeTrajectories, sessionID)

	// 创建返回副本
	result := *trajectory
	result.Steps = make([]*TrajectoryStep, len(trajectory.Steps))
	copy(result.Steps, trajectory.Steps)

	t.logger.Info("轨迹跟踪完成",
		zap.String("trajectory_id", trajectory.ID),
		zap.Duration("duration", trajectory.EndTime.Sub(trajectory.StartTime)),
		zap.Int("step_count", len(trajectory.Steps)),
		zap.Bool("success", trajectory.Success),
	)

	return &result, nil
}

// GetTrajectory 实现 TrajectoryTracker.GetTrajectory
// 获取指定会话的轨迹
func (t *FileBasedTrajectoryTracker) GetTrajectory(
	ctx context.Context,
	sessionID string,
) (*Trajectory, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("会话ID不能为空")
	}

	t.mu.RLock()
	// 首先检查活跃轨迹
	if trajectory, exists := t.activeTrajectories[sessionID]; exists {
		// 创建副本返回
		result := *trajectory
		result.Steps = make([]*TrajectoryStep, len(trajectory.Steps))
		copy(result.Steps, trajectory.Steps)
		t.mu.RUnlock()
		return &result, nil
	}
	t.mu.RUnlock()

	// 从文件加载
	return t.loadTrajectoryFromFile(sessionID)
}

// ListTrajectories 实现 TrajectoryTracker.ListTrajectories
// 列出所有轨迹
func (t *FileBasedTrajectoryTracker) ListTrajectories(
	ctx context.Context,
	agentID string,
	limit int,
) ([]*TrajectoryMetadata, error) {
	var metadataList []*TrajectoryMetadata

	// 收集活跃轨迹的元数据
	t.mu.RLock()
	for _, trajectory := range t.activeTrajectories {
		if agentID == "" || trajectory.AgentID == agentID {
			metadata := &TrajectoryMetadata{
				ID:        trajectory.ID,
				SessionID: trajectory.SessionID,
				AgentID:   trajectory.AgentID,
				StartTime: trajectory.StartTime,
				Duration:  time.Since(trajectory.StartTime),
				StepCount: len(trajectory.Steps),
				Success:   false, // 未完成的轨迹
				Reward:    trajectory.TotalReward,
			}
			metadataList = append(metadataList, metadata)
		}
	}
	t.mu.RUnlock()

	// 从文件系统收集已保存的轨迹
	savedMetadata, err := t.loadTrajectoryMetadataFromFiles(agentID)
	if err != nil {
		t.logger.Warn("加载轨迹元数据失败", zap.Error(err))
	} else {
		metadataList = append(metadataList, savedMetadata...)
	}

	// 按时间排序（最新的在前）
	sort.Slice(metadataList, func(i, j int) bool {
		return metadataList[i].StartTime.After(metadataList[j].StartTime)
	})

	// 应用限制
	if limit > 0 && limit < len(metadataList) {
		metadataList = metadataList[:limit]
	}

	t.logger.Debug("列出轨迹",
		zap.String("agent_id", agentID),
		zap.Int("total_found", len(metadataList)),
		zap.Int("limit", limit),
	)

	return metadataList, nil
}

// saveTrajectoryToFile 保存轨迹到文件
func (t *FileBasedTrajectoryTracker) saveTrajectoryToFile(trajectory *Trajectory) error {
	fileName := fmt.Sprintf("%s.json", trajectory.ID)
	filePath := filepath.Join(t.basePath, fileName)

	data, err := json.MarshalIndent(trajectory, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化轨迹失败: %w", err)
	}

	// 原子性写入
	tempFile := filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	if err := os.Rename(tempFile, filePath); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("重命名文件失败: %w", err)
	}

	return nil
}

// loadTrajectoryFromFile 从文件加载轨迹
func (t *FileBasedTrajectoryTracker) loadTrajectoryFromFile(sessionID string) (*Trajectory, error) {
	// 尝试不同的轨迹ID格式
	possibleIDs := []string{
		sessionID,
		fmt.Sprintf("trajectory_%s", sessionID),
	}

	for _, id := range possibleIDs {
		fileName := fmt.Sprintf("%s.json", id)
		filePath := filepath.Join(t.basePath, fileName)

		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			continue
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var trajectory Trajectory
		if err := json.Unmarshal(data, &trajectory); err != nil {
			t.logger.Warn("解析轨迹文件失败",
				zap.String("file_path", filePath),
				zap.Error(err),
			)
			continue
		}

		return &trajectory, nil
	}

	return nil, fmt.Errorf("会话 %s 的轨迹文件不存在", sessionID)
}

// loadTrajectoryMetadataFromFiles 从文件加载轨迹元数据
func (t *FileBasedTrajectoryTracker) loadTrajectoryMetadataFromFiles(agentID string) ([]*TrajectoryMetadata, error) {
	var metadataList []*TrajectoryMetadata

	entries, err := os.ReadDir(t.basePath)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(t.basePath, entry.Name())

		// 只读取元数据，不加载完整轨迹
		metadata, err := t.loadTrajectoryMetadataFromFile(filePath, agentID)
		if err != nil {
			t.logger.Warn("加载轨迹元数据失败",
				zap.String("file_path", filePath),
				zap.Error(err),
			)
			continue
		}

		if metadata != nil {
			metadataList = append(metadataList, metadata)
		}
	}

	return metadataList, nil
}

// loadTrajectoryMetadataFromFile 从文件加载轨迹元数据
func (t *FileBasedTrajectoryTracker) loadTrajectoryMetadataFromFile(filePath, agentID string) (*TrajectoryMetadata, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// 只解析需要的字段
	var trajectory struct {
		ID        string    `json:"id"`
		SessionID string    `json:"session_id"`
		AgentID   string    `json:"agent_id"`
		StartTime time.Time `json:"start_time"`
		EndTime   time.Time `json:"end_time"`
		Steps     []any     `json:"steps"`
		Success   bool      `json:"success"`
		TotalReward float64 `json:"total_reward"`
	}

	if err := json.Unmarshal(data, &trajectory); err != nil {
		return nil, err
	}

	// 过滤Agent ID
	if agentID != "" && trajectory.AgentID != agentID {
		return nil, nil
	}

	duration := trajectory.EndTime.Sub(trajectory.StartTime)
	if duration == 0 && !trajectory.EndTime.IsZero() {
		duration = time.Since(trajectory.StartTime)
	}

	return &TrajectoryMetadata{
		ID:        trajectory.ID,
		SessionID: trajectory.SessionID,
		AgentID:   trajectory.AgentID,
		StartTime: trajectory.StartTime,
		Duration:  duration,
		StepCount: len(trajectory.Steps),
		Success:   trajectory.Success,
		Reward:    trajectory.TotalReward,
	}, nil
}

// calculateOverallSuccess 计算轨迹的整体成功状态
func (t *FileBasedTrajectoryTracker) calculateOverallSuccess(trajectory *Trajectory) bool {
	if len(trajectory.Steps) == 0 {
		return false
	}

	successCount := 0
	for _, step := range trajectory.Steps {
		if step.Success {
			successCount++
		}
	}

	// 如果超过80%的步骤成功，认为整体成功
	return float64(successCount)/float64(len(trajectory.Steps)) > 0.8
}

// cleanupLoop 清理循环
func (t *FileBasedTrajectoryTracker) cleanupLoop() {
	ticker := time.NewTicker(t.config.FlushInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := t.cleanup(); err != nil {
			t.logger.Error("清理轨迹失败", zap.Error(err))
		}
	}
}

// cleanup 清理过期轨迹
func (t *FileBasedTrajectoryTracker) cleanup() error {
	if t.config.RetentionDays <= 0 {
		return nil
	}

	cutoff := time.Now().AddDate(0, 0, -t.config.RetentionDays)

	entries, err := os.ReadDir(t.basePath)
	if err != nil {
		return err
	}

	deletedCount := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			filePath := filepath.Join(t.basePath, entry.Name())
			if err := os.Remove(filePath); err != nil {
				t.logger.Warn("删除过期轨迹失败",
					zap.String("file_path", filePath),
					zap.Error(err),
				)
			} else {
				deletedCount++
			}
		}
	}

	if deletedCount > 0 {
		t.logger.Info("清理过期轨迹",
			zap.Int("deleted_count", deletedCount),
			zap.Duration("retention", time.Duration(t.config.RetentionDays)*24*time.Hour),
		)
	}

	return nil
}

// generateTrajectoryID 生成轨迹ID
func generateTrajectoryID(sessionID, agentID string) string {
	return fmt.Sprintf("trajectory_%s_%s_%d", agentID, sessionID, time.Now().Unix())
}