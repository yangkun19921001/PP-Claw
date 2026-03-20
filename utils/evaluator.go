package utils

import (
	"context"
	"encoding/json"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// evaluateNotificationResult is the expected tool call result.
type evaluateNotificationResult struct {
	ShouldNotify bool   `json:"should_notify"`
	Reason       string `json:"reason"`
}

// EvaluateResponse uses an LLM to decide whether a background task result
// warrants notifying the user.
// Returns true if the user should be notified (also returns true on any failure).
func EvaluateResponse(ctx context.Context, response, taskContext string,
	chatModel einomodel.ToolCallingChatModel, logger *zap.Logger) bool {

	if chatModel == nil || response == "" {
		return true
	}

	toolInfo := &schema.ToolInfo{
		Name: "evaluate_notification",
		Desc: "Decide whether to notify the user about this background task result.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"should_notify": {
				Type:     schema.Boolean,
				Desc:     "true if the user should be notified",
				Required: true,
			},
			"reason": {
				Type:     schema.String,
				Desc:     "Brief reason for the decision",
				Required: true,
			},
		}),
	}

	boundModel, err := chatModel.WithTools([]*schema.ToolInfo{toolInfo})
	if err != nil {
		if logger != nil {
			logger.Debug("评估器: 绑定工具失败", zap.Error(err))
		}
		return true
	}

	systemPrompt := `You are a notification evaluator. Decide if the user should be notified about this background task result.

Notify (should_notify=true) when:
- The result contains actionable information
- An error or failure occurred
- A deliverable was completed
- Something unexpected happened

Suppress (should_notify=false) when:
- The result is routine/expected with no action needed
- The response is empty or purely informational
- It's a normal status check with nothing notable

You MUST call the evaluate_notification tool.`

	userContent := "Task context: " + taskContext + "\n\nResult:\n" + response

	messages := []*schema.Message{
		{Role: schema.System, Content: systemPrompt},
		{Role: schema.User, Content: userContent},
	}

	resp, err := boundModel.Generate(ctx, messages)
	if err != nil {
		if logger != nil {
			logger.Debug("评估器: LLM 调用失败，默认通知", zap.Error(err))
		}
		return true
	}

	if len(resp.ToolCalls) == 0 {
		return true
	}

	var result evaluateNotificationResult
	if err := json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &result); err != nil {
		return true
	}

	if logger != nil {
		logger.Debug("评估器决策",
			zap.Bool("should_notify", result.ShouldNotify),
			zap.String("reason", result.Reason),
		)
	}

	return result.ShouldNotify
}
