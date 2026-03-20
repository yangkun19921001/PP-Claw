package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// AzureConfig holds Azure OpenAI configuration.
type AzureConfig struct {
	APIKey       string
	APIBase      string
	DefaultModel string
	APIVersion   string
}

// AzureChatModel implements einomodel.ToolCallingChatModel for Azure OpenAI.
type AzureChatModel struct {
	config     AzureConfig
	httpClient *http.Client
	tools      []*schema.ToolInfo
	logger     *zap.Logger
}

// NewAzureChatModel creates a new Azure OpenAI ChatModel.
func NewAzureChatModel(cfg AzureConfig, logger *zap.Logger) *AzureChatModel {
	if cfg.APIVersion == "" {
		cfg.APIVersion = "2024-10-21"
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AzureChatModel{
		config:     cfg,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		logger:     logger,
	}
}

// isReasoningModel checks if the model is a reasoning model that doesn't support temperature.
func isReasoningModel(m string) bool {
	lower := strings.ToLower(m)
	for _, prefix := range []string{"o1", "o3", "o4", "gpt-5"} {
		if strings.HasPrefix(lower, prefix) || strings.Contains(lower, "/"+prefix) {
			return true
		}
	}
	return false
}

// azureRequest represents the Azure OpenAI chat completions request.
type azureRequest struct {
	Messages           []azureMessage   `json:"messages"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Temperature        *float32         `json:"temperature,omitempty"`
	Tools              []azureTool      `json:"tools,omitempty"`
}

type azureMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []azureToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type azureTool struct {
	Type     string             `json:"type"`
	Function azureToolFunction  `json:"function"`
}

type azureToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type azureToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// azureResponse represents the Azure OpenAI chat completions response.
type azureResponse struct {
	Choices []struct {
		Message struct {
			Role      string          `json:"role"`
			Content   string          `json:"content"`
			ToolCalls []azureToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

// Generate implements model.ToolCallingChatModel.
func (m *AzureChatModel) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	deployment := m.config.DefaultModel
	url := fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		strings.TrimRight(m.config.APIBase, "/"), deployment, m.config.APIVersion)

	// Convert messages
	azureMsgs := make([]azureMessage, 0, len(messages))
	for _, msg := range messages {
		am := azureMessage{Content: msg.Content}
		switch msg.Role {
		case schema.System:
			am.Role = "system"
		case schema.User:
			am.Role = "user"
		case schema.Assistant:
			am.Role = "assistant"
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					am.ToolCalls = append(am.ToolCalls, azureToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					})
				}
			}
		case schema.Tool:
			am.Role = "tool"
			am.ToolCallID = msg.ToolCallID
		default:
			am.Role = "user"
		}
		azureMsgs = append(azureMsgs, am)
	}

	req := azureRequest{
		Messages:            azureMsgs,
		MaxCompletionTokens: 8192,
	}

	// Temperature — skip for reasoning models
	if !isReasoningModel(deployment) {
		temp := float32(0.1)
		req.Temperature = &temp
	}

	// Tools
	if len(m.tools) > 0 {
		for _, t := range m.tools {
			at := azureTool{
				Type: "function",
				Function: azureToolFunction{
					Name:        t.Name,
					Description: t.Desc,
				},
			}
			if t.ParamsOneOf != nil {
				if jsonSchema, err := t.ParamsOneOf.ToJSONSchema(); err == nil && jsonSchema != nil {
					at.Function.Parameters = jsonSchema
				}
			}
			req.Tools = append(req.Tools, at)
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("azure marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("azure request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("api-key", m.config.APIKey)

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("azure http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("azure read: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("azure API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var azureResp azureResponse
	if err := json.Unmarshal(respBody, &azureResp); err != nil {
		return nil, fmt.Errorf("azure unmarshal: %w", err)
	}

	if azureResp.Error != nil {
		return nil, fmt.Errorf("azure error: %s", azureResp.Error.Message)
	}

	if len(azureResp.Choices) == 0 {
		return nil, fmt.Errorf("azure: no choices in response")
	}

	choice := azureResp.Choices[0]
	result := &schema.Message{
		Role:    schema.Assistant,
		Content: choice.Message.Content,
	}

	// Convert tool calls
	for _, tc := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, schema.ToolCall{
			ID: tc.ID,
			Function: schema.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	return result, nil
}

// Stream implements model.ToolCallingChatModel (stub — returns full response).
func (m *AzureChatModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}

	r, w := schema.Pipe[*schema.Message](1)
	go func() {
		defer w.Close()
		w.Send(msg, nil)
	}()
	return r, nil
}

// WithTools returns a new model with the given tools bound.
func (m *AzureChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	newModel := &AzureChatModel{
		config:     m.config,
		httpClient: m.httpClient,
		tools:      tools,
		logger:     m.logger,
	}
	return newModel, nil
}

// BindTools implements model.ToolCallingChatModel.
func (m *AzureChatModel) BindTools(tools []*schema.ToolInfo) error {
	m.tools = tools
	return nil
}
