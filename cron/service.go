package cron

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/google/uuid"
)

// nowMs 获取当前时间戳 (毫秒)
func nowMs() int64 {
	return time.Now().UnixMilli()
}

// computeNextRun 计算下次运行时间 (对标 cron/service.py:_compute_next_run)
func computeNextRun(schedule CronSchedule, now int64) int64 {
	switch schedule.Kind {
	case "at":
		if schedule.AtMs > now {
			return schedule.AtMs
		}
		return 0
	case "every":
		if schedule.EveryMs <= 0 {
			return 0
		}
		return now + schedule.EveryMs
	case "cron":
		// 简化实现: 使用 every 30分钟 作为 cron 近似
		// 实际生产中应使用 croniter 等库
		return now + 30*60*1000
	}
	return 0
}

// OnJobFunc Job 执行回调
type OnJobFunc func(job *CronJob) (string, error)

// Service Cron 服务 (对标 cron/service.py:CronService)
type Service struct {
	storePath string
	onJob     OnJobFunc
	store     *CronStore
	running   bool
	mu        sync.Mutex
	logger    *zap.Logger
	cancel    context.CancelFunc
}

// NewService 创建 Cron 服务
func NewService(storePath string, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		storePath: storePath,
		logger:    logger,
	}
}

// SetOnJob 设置 job 回调
func (s *Service) SetOnJob(fn OnJobFunc) {
	s.onJob = fn
}

// loadStore 从磁盘加载 (对标 service.py:_load_store)
func (s *Service) loadStore() *CronStore {
	if s.store != nil {
		return s.store
	}

	data, err := os.ReadFile(s.storePath)
	if err != nil {
		s.store = &CronStore{Version: 1, Jobs: []CronJob{}}
		return s.store
	}

	var store CronStore
	if err := json.Unmarshal(data, &store); err != nil {
		s.logger.Warn("加载 cron store 失败", zap.Error(err))
		s.store = &CronStore{Version: 1, Jobs: []CronJob{}}
		return s.store
	}
	s.store = &store
	return s.store
}

// saveStore 保存到磁盘 (对标 service.py:_save_store)
func (s *Service) saveStore() {
	if s.store == nil {
		return
	}
	dir := filepath.Dir(s.storePath)
	os.MkdirAll(dir, 0755)

	data, _ := json.MarshalIndent(s.store, "", "  ")
	os.WriteFile(s.storePath, data, 0644)
}

// Start 启动 Cron 服务 (对标 service.py:start)
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	s.running = true
	s.loadStore()
	s.recomputeNextRuns()
	s.saveStore()
	s.mu.Unlock()

	ctx2, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.logger.Info("Cron 服务启动", zap.Int("jobs", len(s.store.Jobs)))
	go s.timerLoop(ctx2)
}

// Stop 停止服务
func (s *Service) Stop() {
	s.running = false
	if s.cancel != nil {
		s.cancel()
	}
}

// recomputeNextRuns 重新计算所有任务的下次运行时间
func (s *Service) recomputeNextRuns() {
	now := nowMs()
	for i := range s.store.Jobs {
		if s.store.Jobs[i].Enabled {
			s.store.Jobs[i].State.NextRunAtMs = computeNextRun(s.store.Jobs[i].Schedule, now)
		}
	}
}

// getNextWakeMs 获取最早的下次唤醒时间
func (s *Service) getNextWakeMs() int64 {
	var earliest int64
	for _, j := range s.store.Jobs {
		if j.Enabled && j.State.NextRunAtMs > 0 {
			if earliest == 0 || j.State.NextRunAtMs < earliest {
				earliest = j.State.NextRunAtMs
			}
		}
	}
	return earliest
}

// timerLoop 定时器循环
func (s *Service) timerLoop(ctx context.Context) {
	for {
		s.mu.Lock()
		nextWake := s.getNextWakeMs()
		s.mu.Unlock()

		if nextWake == 0 || !s.running {
			// 没有待运行任务，每30秒检查一次
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
				continue
			}
		}

		delay := time.Duration(nextWake-nowMs()) * time.Millisecond
		if delay < 0 {
			delay = 0
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			s.onTimer()
		}
	}
}

// onTimer 定时器触发 (对标 service.py:_on_timer)
func (s *Service) onTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := nowMs()
	for i := range s.store.Jobs {
		j := &s.store.Jobs[i]
		if j.Enabled && j.State.NextRunAtMs > 0 && now >= j.State.NextRunAtMs {
			s.executeJob(j)
		}
	}
	s.saveStore()
}

// executeJob 执行任务 (对标 service.py:_execute_job)
func (s *Service) executeJob(job *CronJob) {
	startMs := nowMs()
	s.logger.Info("Cron: 执行任务", zap.String("name", job.Name), zap.String("id", job.ID))

	if s.onJob != nil {
		_, err := s.onJob(job)
		if err != nil {
			job.State.LastStatus = "error"
			job.State.LastError = err.Error()
			s.logger.Error("Cron: 任务失败", zap.String("name", job.Name), zap.Error(err))
		} else {
			job.State.LastStatus = "ok"
			job.State.LastError = ""
		}
	}

	job.State.LastRunAtMs = startMs
	job.UpdatedAtMs = nowMs()

	// 一次性任务处理
	if job.Schedule.Kind == "at" {
		if job.DeleteAfterRun {
			s.removeJobByID(job.ID)
		} else {
			job.Enabled = false
			job.State.NextRunAtMs = 0
		}
	} else {
		job.State.NextRunAtMs = computeNextRun(job.Schedule, nowMs())
	}
}

// ========== Public API ==========

// ListJobs 列出所有任务 (对标 service.py:list_jobs)
func (s *Service) ListJobs(includeDisabled bool) []CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	store := s.loadStore()
	if includeDisabled {
		return store.Jobs
	}
	var result []CronJob
	for _, j := range store.Jobs {
		if j.Enabled {
			result = append(result, j)
		}
	}
	return result
}

// AddJob 添加任务 (对标 service.py:add_job)
func (s *Service) AddJob(name string, schedule CronSchedule, message string, deliver bool, channel, to string, deleteAfterRun bool) *CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	store := s.loadStore()
	now := nowMs()

	job := CronJob{
		ID:       uuid.New().String()[:8],
		Name:     name,
		Enabled:  true,
		Schedule: schedule,
		Payload: CronPayload{
			Kind:    "agent_turn",
			Message: message,
			Deliver: deliver,
			Channel: channel,
			To:      to,
		},
		State:          CronJobState{NextRunAtMs: computeNextRun(schedule, now)},
		CreatedAtMs:    now,
		UpdatedAtMs:    now,
		DeleteAfterRun: deleteAfterRun,
	}

	store.Jobs = append(store.Jobs, job)
	s.saveStore()
	s.logger.Info("Cron: 添加任务", zap.String("name", name), zap.String("id", job.ID))
	return &job
}

// RemoveJob 删除任务 (对标 service.py:remove_job)
func (s *Service) RemoveJob(jobID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.removeJobByID(jobID)
}

func (s *Service) removeJobByID(jobID string) bool {
	store := s.loadStore()
	before := len(store.Jobs)
	var filtered []CronJob
	for _, j := range store.Jobs {
		if j.ID != jobID {
			filtered = append(filtered, j)
		}
	}
	store.Jobs = filtered
	removed := len(store.Jobs) < before

	if removed {
		s.saveStore()
		s.logger.Info("Cron: 删除任务", zap.String("id", jobID))
	}
	return removed
}

// EnableJob 启用/禁用任务 (对标 service.py:enable_job)
func (s *Service) EnableJob(jobID string, enabled bool) *CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	store := s.loadStore()
	for i := range store.Jobs {
		if store.Jobs[i].ID == jobID {
			store.Jobs[i].Enabled = enabled
			store.Jobs[i].UpdatedAtMs = nowMs()
			if enabled {
				store.Jobs[i].State.NextRunAtMs = computeNextRun(store.Jobs[i].Schedule, nowMs())
			} else {
				store.Jobs[i].State.NextRunAtMs = 0
			}
			s.saveStore()
			s.logger.Info("Cron: 更新任务状态",
				zap.String("id", jobID),
				zap.Bool("enabled", enabled),
			)
			job := store.Jobs[i]
			return &job
		}
	}
	return nil
}

// Status 获取服务状态 (对标 service.py:status)
func (s *Service) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	store := s.loadStore()
	return map[string]any{
		"enabled":         s.running,
		"jobs":            len(store.Jobs),
		"next_wake_at_ms": s.getNextWakeMs(),
	}
}
