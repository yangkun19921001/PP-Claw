//go:build amd64 || arm64 || riscv64 || mips64 || mips64le || ppc64

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkaily "github.com/larksuite/oapi-sdk-go/v3/service/aily/v1"
)

func init() {
	RegisterFeishuToolFactory(func(cfg *FeishuToolsConfig) Tool {
		if cfg.AilyAppID == "" {
			return nil
		}
		return &FeishuKnowledgeTool{
			Client:          lark.NewClient(cfg.AppID, cfg.AppSecret),
			AilyAppID:       cfg.AilyAppID,
			DataAssetIDs:    cfg.AilyDataAssetIDs,
			DataAssetTagIDs: cfg.AilyDataAssetTagIDs,
		}
	})
}

// FeishuKnowledgeTool 飞书 Aily 数据知识问答工具
type FeishuKnowledgeTool struct {
	Client          *lark.Client
	AilyAppID       string   // 飞书智能伙伴 App ID
	DataAssetIDs    []string // 默认数据知识 ID 列表
	DataAssetTagIDs []string // 默认数据知识分类 ID 列表
}

func (t *FeishuKnowledgeTool) Name() string { return "feishu_knowledge" }

func (t *FeishuKnowledgeTool) Description() string {
	return "Ask questions against Feishu/Lark Aily knowledge base. The AI will generate answers based on configured data knowledge."
}

func (t *FeishuKnowledgeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "The question to ask the knowledge base",
			},
			"data_asset_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Data asset IDs to scope the knowledge query (optional, uses default if not specified)",
			},
			"data_asset_tag_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Data asset tag IDs to scope the knowledge query (optional, uses default if not specified)",
			},
		},
		"required": []any{"question"},
	}
}

func (t *FeishuKnowledgeTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	question, _ := params["question"].(string)
	if question == "" {
		return "", fmt.Errorf("question is required")
	}

	// 使用参数中的 ID 列表，否则使用配置默认值
	dataAssetIDs := t.DataAssetIDs
	if ids, ok := params["data_asset_ids"].([]any); ok && len(ids) > 0 {
		dataAssetIDs = toStringSlice(ids)
	}
	dataAssetTagIDs := t.DataAssetTagIDs
	if ids, ok := params["data_asset_tag_ids"].([]any); ok && len(ids) > 0 {
		dataAssetTagIDs = toStringSlice(ids)
	}

	// 构建请求
	bodyBuilder := larkaily.NewAskAppKnowledgeReqBodyBuilder().
		Message(larkaily.NewAilyKnowledgeMessageBuilder().
			Content(question).
			Build())

	if len(dataAssetIDs) > 0 {
		bodyBuilder.DataAssetIds(dataAssetIDs)
	}
	if len(dataAssetTagIDs) > 0 {
		bodyBuilder.DataAssetTagIds(dataAssetTagIDs)
	}

	req := larkaily.NewAskAppKnowledgeReqBuilder().
		AppId(t.AilyAppID).
		Body(bodyBuilder.Build()).
		Build()

	resp, err := t.Client.Aily.V1.AppKnowledge.Ask(ctx, req)
	if err != nil {
		return "", fmt.Errorf("knowledge ask failed: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("knowledge ask failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	return formatKnowledgeResp(resp.Data), nil
}

// formatKnowledgeResp 格式化知识问答响应
func formatKnowledgeResp(data *larkaily.AskAppKnowledgeRespData) string {
	if data == nil {
		return "未获取到回答。"
	}

	var sb strings.Builder

	// 回答内容
	if data.Message != nil && data.Message.Content != nil {
		sb.WriteString(*data.Message.Content)
	}

	// 元信息
	var meta []string
	if data.Status != nil {
		meta = append(meta, fmt.Sprintf("status=%s", *data.Status))
	}
	if data.FinishType != nil {
		meta = append(meta, fmt.Sprintf("finish_type=%s", *data.FinishType))
	}
	if data.HasAnswer != nil {
		meta = append(meta, fmt.Sprintf("has_answer=%v", *data.HasAnswer))
	}

	// FAQ 命中
	if data.FaqResult != nil {
		if data.FaqResult.Question != nil {
			sb.WriteString(fmt.Sprintf("\n\n[FAQ 匹配] %s", *data.FaqResult.Question))
		}
		if data.FaqResult.Answer != nil {
			sb.WriteString(fmt.Sprintf("\n%s", *data.FaqResult.Answer))
		}
	}

	// 过程数据（chunks、图表等）
	if data.ProcessData != nil {
		pd := data.ProcessData
		if len(pd.Chunks) > 0 {
			sb.WriteString("\n\n---\n**参考知识片段：**\n")
			for i, chunk := range pd.Chunks {
				sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, chunk))
			}
		}
		if len(pd.ChartDsls) > 0 {
			sb.WriteString("\n**图表：**\n")
			for _, chart := range pd.ChartDsls {
				sb.WriteString(chart + "\n")
			}
		}
		if len(pd.SqlData) > 0 {
			sb.WriteString("\n**数据查询结果：**\n")
			for _, d := range pd.SqlData {
				sb.WriteString(d + "\n")
			}
		}
	}

	if len(meta) > 0 {
		sb.WriteString(fmt.Sprintf("\n\n[%s]", strings.Join(meta, ", ")))
	}

	result := sb.String()
	if result == "" {
		out, _ := json.MarshalIndent(data, "", "  ")
		return string(out)
	}
	return result
}

func toStringSlice(items []any) []string {
	var result []string
	for _, item := range items {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
