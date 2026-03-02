package heartbeat

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

// OnExecuteFunc 执行回调：将任务交给 Agent 处理
type OnExecuteFunc func(tasks string) (string, error)

// OnNotifyFunc 通知回调：将结果投递到渠道
type OnNotifyFunc func(response string) error

// Service Heartbeat 服务 (对标 heartbeat/service.py:HeartbeatService)
type Service struct {
	workspace string
	onExecute OnExecuteFunc
	onNotify  OnNotifyFunc
	intervalS int
	enabled   bool
	running   bool
	cancel    context.CancelFunc
	logger    *zap.Logger
}

// NewService 创建 Heartbeat 服务
func NewService(workspace string, intervalS int, enabled bool, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		workspace: workspace,
		intervalS: intervalS,
		enabled:   enabled,
		logger:    logger,
	}
}

// SetCallbacks 设置回调
func (s *Service) SetCallbacks(onExecute OnExecuteFunc, onNotify OnNotifyFunc) {
	s.onExecute = onExecute
	s.onNotify = onNotify
}

// heartbeatFile HEARTBEAT.md 路径
func (s *Service) heartbeatFile() string {
	return filepath.Join(s.workspace, "HEARTBEAT.md")
}

// readHeartbeatFile 读取 HEARTBEAT.md 内容
func (s *Service) readHeartbeatFile() string {
	data, err := os.ReadFile(s.heartbeatFile())
	if err != nil {
		return ""
	}
	return string(data)
}

// Start 启动 Heartbeat 服务 (对标 service.py:start)
func (s *Service) Start(ctx context.Context) {
	if !s.enabled {
		s.logger.Info("Heartbeat 已禁用")
		return
	}
	if s.running {
		s.logger.Warn("Heartbeat 已在运行")
		return
	}

	s.running = true
	ctx2, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.logger.Info("Heartbeat 启动", zap.Int("interval_s", s.intervalS))
	go s.runLoop(ctx2)
}

// Stop 停止服务
func (s *Service) Stop() {
	s.running = false
	if s.cancel != nil {
		s.cancel()
	}
}

// runLoop 主循环 (对标 service.py:_run_loop)
func (s *Service) runLoop(ctx context.Context) {
	for s.running {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(s.intervalS) * time.Second):
			if s.running {
				s.tick()
			}
		}
	}
}

// tick 单次心跳 (对标 service.py:_tick)
func (s *Service) tick() {
	content := s.readHeartbeatFile()
	if content == "" {
		s.logger.Debug("Heartbeat: HEARTBEAT.md 不存在或为空")
		return
	}

	s.logger.Info("Heartbeat: 检查任务...")

	// 简化实现: 直接执行 (完整实现应使用 LLM decision phase)
	if s.onExecute != nil {
		response, err := s.onExecute(content)
		if err != nil {
			s.logger.Error("Heartbeat: 执行失败", zap.Error(err))
			return
		}
		if response != "" && s.onNotify != nil {
			s.logger.Info("Heartbeat: 完成，投递响应")
			s.onNotify(response)
		}
	}
}

// TriggerNow 手动触发 (对标 service.py:trigger_now)
func (s *Service) TriggerNow() (string, error) {
	content := s.readHeartbeatFile()
	if content == "" || s.onExecute == nil {
		return "", nil
	}
	return s.onExecute(content)
}
