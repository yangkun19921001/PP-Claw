package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// DelegateFunc 委托执行函数签名：同步调用目标 Agent 完成任务并返回结果。
type DelegateFunc func(ctx context.Context, targetAgentID, task string) (string, error)

// DelegateTool 允许 Agent 委托任务给其他 Agent（同步 in-process 调用）。
// AllowedTargets 由配置 delegates_to 决定，限制可调用的目标 Agent。
type DelegateTool struct {
	AllowedTargets []string     // 可委托的目标 Agent ID（展示为 enum）
	Delegate       DelegateFunc // 实际执行委托的回调
}

func (t *DelegateTool) Name() string { return "delegate" }

func (t *DelegateTool) Description() string {
	return fmt.Sprintf(
		"Delegate a task to another specialized agent and get the result. Available agents: %s. "+
			"Use this when the task requires expertise from a different agent.",
		strings.Join(t.AllowedTargets, ", "),
	)
}

func (t *DelegateTool) Parameters() map[string]any {
	targetEnum := make([]any, len(t.AllowedTargets))
	for i, id := range t.AllowedTargets {
		targetEnum[i] = id
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_agent": map[string]any{
				"type":        "string",
				"enum":        targetEnum,
				"description": "The ID of the target agent to delegate the task to",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "A clear description of the task for the target agent to perform",
			},
		},
		"required": []any{"target_agent", "task"},
	}
}

func (t *DelegateTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	targetAgent, _ := params["target_agent"].(string)
	task, _ := params["task"].(string)

	if targetAgent == "" {
		return "", fmt.Errorf("target_agent is required")
	}
	if task == "" {
		return "", fmt.Errorf("task is required")
	}

	// 校验目标在白名单中
	if !slices.Contains(t.AllowedTargets, targetAgent) {
		return "", fmt.Errorf("agent %q is not in the allowed delegation targets: %v", targetAgent, t.AllowedTargets)
	}

	result, err := t.Delegate(ctx, targetAgent, task)
	if err != nil {
		return "", fmt.Errorf("delegation to %q failed: %w", targetAgent, err)
	}

	return result, nil
}
