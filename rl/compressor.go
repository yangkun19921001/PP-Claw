// Package rl - 轨迹压缩器的具体实现
// 遵循 Google Go 编程规范，实现 TrajectoryCompressor 接口
package rl

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	einomodel "github.com/cloudwego/eino/components/model"
	"go.uber.org/zap"
)

// DefaultTrajectoryCompressor 默认轨迹压缩器实现
// 对应 Hermes-Agent 的轨迹压缩功能，保护重要部分并压缩中间区域
type DefaultTrajectoryCompressor struct {
	model       einomodel.ToolCallingChatModel // 用于生成摘要的LLM模型
	logger      *zap.Logger                    // 日志器
	stats       *CompressionStats              // 压缩统计
	statsMu     sync.RWMutex                   // 统计锁
	semaphore   chan struct{}                  // 并发控制信号量
}

// NewDefaultTrajectoryCompressor 创建默认轨迹压缩器
func NewDefaultTrajectoryCompressor(
	model einomodel.ToolCallingChatModel,
	logger *zap.Logger,
	maxConcurrency int,
) *DefaultTrajectoryCompressor {
	if logger == nil {
		logger = zap.NewNop()
	}

	if maxConcurrency <= 0 {
		maxConcurrency = 5 // 默认最多5个并发
	}

	return &DefaultTrajectoryCompressor{
		model:     model,
		logger:    logger,
		stats:     &CompressionStats{},
		semaphore: make(chan struct{}, maxConcurrency),
	}
}

// CompressTrajectory 实现 TrajectoryCompressor.CompressTrajectory
// 压缩轨迹，保护重要部分
func (c *DefaultTrajectoryCompressor) CompressTrajectory(
	ctx context.Context,
	trajectory *Trajectory,
	config *CompressionConfig,
) (*CompressedTrajectory, error) {
	if trajectory == nil {
		return nil, fmt.Errorf("轨迹不能为空")
	}

	if !c.ShouldCompress(trajectory, config) {
		// 不需要压缩，直接返回原轨迹
		return &CompressedTrajectory{
			Trajectory:       trajectory,
			CompressedSteps:  trajectory.Steps,
			ProtectedRegions: []ProtectedRegion{},
			CompressionRatio: 1.0,
			CompressionTime:  time.Now(),
		}, nil
	}

	start := time.Now()

	c.logger.Info("开始轨迹压缩",
		zap.String("trajectory_id", trajectory.ID),
		zap.Int("original_steps", len(trajectory.Steps)),
		zap.Int("target_max_steps", config.TargetMaxSteps),
	)

	// 1. 识别保护区域
	protectedRegions := c.identifyProtectedRegions(trajectory.Steps, config)

	// 2. 计算压缩区域
	compressionRegions := c.calculateCompressionRegions(trajectory.Steps, protectedRegions)

	// 3. 并行生成摘要
	summaries, err := c.parallelSummarize(ctx, trajectory.Steps, compressionRegions, config)
	if err != nil {
		return nil, fmt.Errorf("生成摘要失败: %w", err)
	}

	// 4. 重组轨迹
	compressedSteps := c.reconstructTrajectory(trajectory.Steps, protectedRegions, summaries)

	// 5. 计算压缩比率
	compressionRatio := float64(len(compressedSteps)) / float64(len(trajectory.Steps))
	compressionTime := time.Since(start)

	// 6. 更新统计
	c.updateStats(len(trajectory.Steps), len(compressedSteps), compressionTime)

	result := &CompressedTrajectory{
		Trajectory:       trajectory,
		CompressedSteps:  compressedSteps,
		ProtectedRegions: protectedRegions,
		CompressionRatio: compressionRatio,
		CompressionTime:  time.Now(),
	}

	c.logger.Info("轨迹压缩完成",
		zap.String("trajectory_id", trajectory.ID),
		zap.Int("original_steps", len(trajectory.Steps)),
		zap.Int("compressed_steps", len(compressedSteps)),
		zap.Float64("compression_ratio", compressionRatio),
		zap.Duration("compression_time", compressionTime),
	)

	return result, nil
}

// ShouldCompress 实现 TrajectoryCompressor.ShouldCompress
// 判断轨迹是否需要压缩
func (c *DefaultTrajectoryCompressor) ShouldCompress(
	trajectory *Trajectory,
	config *CompressionConfig,
) bool {
	if trajectory == nil || config == nil {
		return false
	}

	// 检查步骤数量是否超过目标
	if len(trajectory.Steps) <= config.TargetMaxSteps {
		return false
	}

	// 检查最小压缩比率
	if config.MinCompressionRatio > 0 {
		potentialRatio := float64(config.TargetMaxSteps) / float64(len(trajectory.Steps))
		if potentialRatio > config.MinCompressionRatio {
			return false
		}
	}

	return true
}

// GetCompressionStats 实现 TrajectoryCompressor.GetCompressionStats
// 获取压缩统计信息
func (c *DefaultTrajectoryCompressor) GetCompressionStats() *CompressionStats {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()

	// 返回统计副本
	return &CompressionStats{
		TotalCompressed:         c.stats.TotalCompressed,
		AverageCompressionRatio: c.stats.AverageCompressionRatio,
		AverageCompressionTime:  c.stats.AverageCompressionTime,
		TokensSaved:             c.stats.TokensSaved,
	}
}

// identifyProtectedRegions 识别保护区域
// 对应 Hermes 的保护策略
func (c *DefaultTrajectoryCompressor) identifyProtectedRegions(
	steps []*TrajectoryStep,
	config *CompressionConfig,
) []ProtectedRegion {
	var regions []ProtectedRegion
	stepCount := len(steps)

	if stepCount == 0 {
		return regions
	}

	// 保护前N步
	if config.ProtectedSteps.FirstN > 0 {
		end := config.ProtectedSteps.FirstN
		if end > stepCount {
			end = stepCount
		}
		regions = append(regions, ProtectedRegion{
			StartIndex: 0,
			EndIndex:   end - 1,
			Reason:     "first_n_steps",
		})
	}

	// 保护后N步
	if config.ProtectedSteps.LastN > 0 {
		start := stepCount - config.ProtectedSteps.LastN
		if start < 0 {
			start = 0
		}
		// 确保不与前N步重叠
		if len(regions) == 0 || start > regions[len(regions)-1].EndIndex {
			regions = append(regions, ProtectedRegion{
				StartIndex: start,
				EndIndex:   stepCount - 1,
				Reason:     "last_n_steps",
			})
		}
	}

	// 保护错误步骤及其恢复
	if config.ProtectedSteps.ProtectError {
		errorRegions := c.findErrorRecoveryRegions(steps)
		regions = append(regions, errorRegions...)
	}

	// 保护重要工具调用
	if config.ProtectedSteps.ProtectTool {
		toolRegions := c.findImportantToolRegions(steps)
		regions = append(regions, toolRegions...)
	}

	// 合并重叠区域
	regions = c.mergeOverlappingRegions(regions)

	c.logger.Debug("识别保护区域",
		zap.Int("total_steps", stepCount),
		zap.Int("protected_regions", len(regions)),
	)

	return regions
}

// findErrorRecoveryRegions 查找错误恢复区域
func (c *DefaultTrajectoryCompressor) findErrorRecoveryRegions(steps []*TrajectoryStep) []ProtectedRegion {
	var regions []ProtectedRegion

	for i, step := range steps {
		if !step.Success {
			// 保护错误步骤及其前后各1步
			start := i - 1
			if start < 0 {
				start = 0
			}
			end := i + 1
			if end >= len(steps) {
				end = len(steps) - 1
			}

			regions = append(regions, ProtectedRegion{
				StartIndex: start,
				EndIndex:   end,
				Reason:     "error_recovery",
			})
		}
	}

	return regions
}

// findImportantToolRegions 查找重要工具调用区域
func (c *DefaultTrajectoryCompressor) findImportantToolRegions(steps []*TrajectoryStep) []ProtectedRegion {
	var regions []ProtectedRegion

	// 重要工具类型
	importantTools := map[string]bool{
		"delegate":     true,
		"spawn":        true,
		"web_search":   true,
		"exec":         true,
		"write_file":   true,
	}

	for i, step := range steps {
		if importantTools[step.Action] {
			regions = append(regions, ProtectedRegion{
				StartIndex: i,
				EndIndex:   i,
				Reason:     "important_tool",
			})
		}
	}

	return regions
}

// mergeOverlappingRegions 合并重叠的保护区域
func (c *DefaultTrajectoryCompressor) mergeOverlappingRegions(regions []ProtectedRegion) []ProtectedRegion {
	if len(regions) <= 1 {
		return regions
	}

	// 按起始位置排序
	for i := 0; i < len(regions)-1; i++ {
		for j := i + 1; j < len(regions); j++ {
			if regions[i].StartIndex > regions[j].StartIndex {
				regions[i], regions[j] = regions[j], regions[i]
			}
		}
	}

	var merged []ProtectedRegion
	current := regions[0]

	for i := 1; i < len(regions); i++ {
		next := regions[i]
		if next.StartIndex <= current.EndIndex+1 {
			// 合并区域
			if next.EndIndex > current.EndIndex {
				current.EndIndex = next.EndIndex
			}
			current.Reason += "," + next.Reason
		} else {
			merged = append(merged, current)
			current = next
		}
	}
	merged = append(merged, current)

	return merged
}

// calculateCompressionRegions 计算需要压缩的区域
func (c *DefaultTrajectoryCompressor) calculateCompressionRegions(
	steps []*TrajectoryStep,
	protectedRegions []ProtectedRegion,
) []CompressionRegion {
	var compressionRegions []CompressionRegion
	stepCount := len(steps)
	lastEnd := -1

	for _, protected := range protectedRegions {
		// 检查保护区域前是否有可压缩区域
		if protected.StartIndex > lastEnd+1 {
			compressionRegions = append(compressionRegions, CompressionRegion{
				StartIndex: lastEnd + 1,
				EndIndex:   protected.StartIndex - 1,
			})
		}
		lastEnd = protected.EndIndex
	}

	// 检查最后一个保护区域后是否有可压缩区域
	if lastEnd+1 < stepCount {
		compressionRegions = append(compressionRegions, CompressionRegion{
			StartIndex: lastEnd + 1,
			EndIndex:   stepCount - 1,
		})
	}

	return compressionRegions
}

// CompressionRegion 压缩区域
type CompressionRegion struct {
	StartIndex int // 开始索引
	EndIndex   int // 结束索引
}

// parallelSummarize 并行生成摘要
// 对应 Hermes 的并发摘要生成
func (c *DefaultTrajectoryCompressor) parallelSummarize(
	ctx context.Context,
	steps []*TrajectoryStep,
	regions []CompressionRegion,
	config *CompressionConfig,
) (map[int]*TrajectoryStep, error) {
	summaries := make(map[int]*TrajectoryStep)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(regions))

	for _, region := range regions {
		wg.Add(1)
		go func(r CompressionRegion) {
			defer wg.Done()

			// 获取信号量
			select {
			case c.semaphore <- struct{}{}:
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			}
			defer func() { <-c.semaphore }()

			summary, err := c.summarizeRegion(ctx, steps, r, config)
			if err != nil {
				errChan <- err
				return
			}

			mu.Lock()
			summaries[r.StartIndex] = summary
			mu.Unlock()
		}(region)
	}

	wg.Wait()
	close(errChan)

	// 检查错误
	if len(errChan) > 0 {
		return nil, <-errChan
	}

	return summaries, nil
}

// summarizeRegion 对区域生成摘要
func (c *DefaultTrajectoryCompressor) summarizeRegion(
	ctx context.Context,
	steps []*TrajectoryStep,
	region CompressionRegion,
	config *CompressionConfig,
) (*TrajectoryStep, error) {
	// 提取区域步骤
	regionSteps := steps[region.StartIndex : region.EndIndex+1]

	// 构建摘要提示
	prompt := c.buildSummarizationPrompt(regionSteps, config.SummaryTargetLength)

	// 调用 LLM 生成摘要
	messages := []*schema.Message{
		{Role: schema.User, Content: prompt},
	}

	response, err := c.model.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM 生成摘要失败: %w", err)
	}

	// 创建摘要步骤
	summary := &TrajectoryStep{
		StepID:       fmt.Sprintf("summary_%d_%d", region.StartIndex, region.EndIndex),
		Timestamp:    regionSteps[0].Timestamp,
		Input:        fmt.Sprintf("摘要了 %d 个步骤", len(regionSteps)),
		Thought:      response.Content,
		Action:       "summary",
		ActionInput:  map[string]any{"compressed_steps": len(regionSteps)},
		Observation:  fmt.Sprintf("压缩了 %d 个步骤为摘要", len(regionSteps)),
		Success:      true,
		Duration:     c.calculateTotalDuration(regionSteps),
		TokensUsed:   c.calculateTotalTokens(regionSteps),
		Metadata: map[string]any{
			"is_summary":      true,
			"original_steps":  len(regionSteps),
			"start_index":     region.StartIndex,
			"end_index":       region.EndIndex,
		},
	}

	return summary, nil
}

// buildSummarizationPrompt 构建摘要提示词
func (c *DefaultTrajectoryCompressor) buildSummarizationPrompt(
	steps []*TrajectoryStep,
	targetLength int,
) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf(
		"请将以下 %d 个执行步骤压缩为一个简洁的摘要（目标长度：%d 字符以内）:\n\n",
		len(steps), targetLength,
	))

	for i, step := range steps {
		builder.WriteString(fmt.Sprintf("步骤 %d:\n", i+1))
		builder.WriteString(fmt.Sprintf("- 动作: %s\n", step.Action))
		if step.Thought != "" {
			builder.WriteString(fmt.Sprintf("- 思考: %s\n", step.Thought))
		}
		if step.Observation != "" {
			builder.WriteString(fmt.Sprintf("- 结果: %s\n", step.Observation))
		}
		builder.WriteString(fmt.Sprintf("- 成功: %t\n", step.Success))
		builder.WriteString("\n")
	}

	builder.WriteString("要求：\n")
	builder.WriteString("1. 保留关键的执行逻辑和结果\n")
	builder.WriteString("2. 突出重要的决策点和错误处理\n")
	builder.WriteString("3. 使用简洁清晰的语言\n")
	builder.WriteString("4. 保持技术准确性\n")

	return builder.String()
}

// reconstructTrajectory 重组轨迹
func (c *DefaultTrajectoryCompressor) reconstructTrajectory(
	originalSteps []*TrajectoryStep,
	protectedRegions []ProtectedRegion,
	summaries map[int]*TrajectoryStep,
) []*TrajectoryStep {
	var result []*TrajectoryStep
	stepIndex := 0

	for stepIndex < len(originalSteps) {
		// 检查当前位置是否在保护区域内
		inProtected := false
		var protectedEnd int

		for _, region := range protectedRegions {
			if stepIndex >= region.StartIndex && stepIndex <= region.EndIndex {
				// 添加保护区域的所有步骤
				for i := region.StartIndex; i <= region.EndIndex && i < len(originalSteps); i++ {
					result = append(result, originalSteps[i])
				}
				protectedEnd = region.EndIndex
				inProtected = true
				break
			}
		}

		if inProtected {
			stepIndex = protectedEnd + 1
			continue
		}

		// 检查是否有摘要
		if summary, exists := summaries[stepIndex]; exists {
			result = append(result, summary)
			// 跳过摘要覆盖的步骤
			if originalStepsCount, ok := summary.Metadata["original_steps"].(int); ok {
				stepIndex += originalStepsCount
			} else {
				stepIndex++
			}
		} else {
			// 保留原始步骤
			result = append(result, originalSteps[stepIndex])
			stepIndex++
		}
	}

	return result
}

// calculateTotalDuration 计算区域总时长
func (c *DefaultTrajectoryCompressor) calculateTotalDuration(steps []*TrajectoryStep) time.Duration {
	var total time.Duration
	for _, step := range steps {
		total += step.Duration
	}
	return total
}

// calculateTotalTokens 计算区域总token数
func (c *DefaultTrajectoryCompressor) calculateTotalTokens(steps []*TrajectoryStep) int {
	total := 0
	for _, step := range steps {
		total += step.TokensUsed
	}
	return total
}

// updateStats 更新压缩统计
func (c *DefaultTrajectoryCompressor) updateStats(
	originalSteps, compressedSteps int,
	compressionTime time.Duration,
) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()

	c.stats.TotalCompressed++
	compressionRatio := float64(compressedSteps) / float64(originalSteps)

	// 更新平均压缩比率
	if c.stats.TotalCompressed == 1 {
		c.stats.AverageCompressionRatio = compressionRatio
	} else {
		total := float64(c.stats.TotalCompressed)
		c.stats.AverageCompressionRatio = (c.stats.AverageCompressionRatio*(total-1) + compressionRatio) / total
	}

	// 更新平均压缩时间
	if c.stats.TotalCompressed == 1 {
		c.stats.AverageCompressionTime = compressionTime
	} else {
		total := float64(c.stats.TotalCompressed)
		avgNanos := (float64(c.stats.AverageCompressionTime.Nanoseconds())*(total-1) + float64(compressionTime.Nanoseconds())) / total
		c.stats.AverageCompressionTime = time.Duration(int64(avgNanos))
	}

	// 更新节省的token数
	if originalSteps > compressedSteps {
		saved := originalSteps - compressedSteps
		// 估算每步平均token数（假设为50）
		c.stats.TokensSaved += saved * 50
	}
}