package tools

import (
	"context"
	"fmt"
)

// SpawnTool 生成子代理工具 (对标 agent/tools/spawn.py:SpawnTool)
type SpawnTool struct {
	SpawnFunc     func(ctx context.Context, task, label, channel, chatID string) string
	originChannel string
	originChatID  string
}

func (t *SpawnTool) Name() string { return "spawn" }
func (t *SpawnTool) Description() string {
	return "Spawn a subagent to handle a task in the background. " +
		"Use this for complex or time-consuming tasks that can run independently. " +
		"The subagent will complete the task and report back when done."
}

func (t *SpawnTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "The task for the subagent to complete",
			},
			"label": map[string]any{
				"type":        "string",
				"description": "Optional short label for the task (for display)",
			},
		},
		"required": []any{"task"},
	}
}

func (t *SpawnTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	task, _ := params["task"].(string)
	label, _ := params["label"].(string)

	if task == "" {
		return "Error: task is required", nil
	}

	channel := t.originChannel
	if channel == "" {
		channel = "cli"
	}
	chatID := t.originChatID
	if chatID == "" {
		chatID = "direct"
	}

	if t.SpawnFunc == nil {
		return "Subagent spawning is not configured", nil
	}
	return t.SpawnFunc(ctx, task, label, channel, chatID), nil
}

// SetContext 实现 ContextSetter 接口
func (t *SpawnTool) SetContext(channel, chatID string) {
	t.originChannel = channel
	t.originChatID = chatID
}

var _ Tool = (*SpawnTool)(nil)

func init() {
	// Verify SpawnTool implements Tool at compile time
	_ = fmt.Sprint
}
