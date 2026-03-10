//go:build amd64 || arm64 || riscv64 || mips64 || mips64le || ppc64

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

func init() {
	RegisterFeishuToolFactory(func(cfg *FeishuToolsConfig) Tool {
		return &FeishuDocsTool{Client: lark.NewClient(cfg.AppID, cfg.AppSecret)}
	})
}

// FeishuDocsTool 飞书文档工具
type FeishuDocsTool struct {
	Client *lark.Client
}

func (t *FeishuDocsTool) Name() string { return "feishu_docs" }

func (t *FeishuDocsTool) Description() string {
	return "Read Feishu/Lark documents. Actions: read, info, list_blocks."
}

func (t *FeishuDocsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform: read, info, list_blocks",
				"enum":        []any{"read", "info", "list_blocks"},
			},
			"document_id": map[string]any{
				"type":        "string",
				"description": "Document ID (required for all actions)",
			},
			"max_length": map[string]any{
				"type":        "integer",
				"description": "Maximum content length to return (default 30000, for read action)",
			},
		},
		"required": []any{"action", "document_id"},
	}
}

func (t *FeishuDocsTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	action, _ := params["action"].(string)
	switch action {
	case "read":
		return t.readDocument(ctx, params)
	case "info":
		return t.getDocumentInfo(ctx, params)
	case "list_blocks":
		return t.listBlocks(ctx, params)
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

func (t *FeishuDocsTool) readDocument(ctx context.Context, params map[string]any) (string, error) {
	docID, _ := params["document_id"].(string)
	if docID == "" {
		return "", fmt.Errorf("document_id is required")
	}

	maxLength := 30000
	if ml, ok := params["max_length"].(float64); ok && ml > 0 {
		maxLength = int(ml)
	}

	req := larkdocx.NewRawContentDocumentReqBuilder().DocumentId(docID).Lang(0).Build()
	resp, err := t.Client.Docx.Document.RawContent(ctx, req)
	if err != nil {
		return "", fmt.Errorf("read document failed: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("read document failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	content := ""
	if resp.Data.Content != nil {
		content = *resp.Data.Content
	}

	if len(content) > maxLength {
		content = content[:maxLength] + "\n\n... (content truncated)"
	}

	return content, nil
}

func (t *FeishuDocsTool) getDocumentInfo(ctx context.Context, params map[string]any) (string, error) {
	docID, _ := params["document_id"].(string)
	if docID == "" {
		return "", fmt.Errorf("document_id is required")
	}

	req := larkdocx.NewGetDocumentReqBuilder().DocumentId(docID).Build()
	resp, err := t.Client.Docx.Document.Get(ctx, req)
	if err != nil {
		return "", fmt.Errorf("get document info failed: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("get document info failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	doc := resp.Data.Document
	info := map[string]any{}
	if doc.DocumentId != nil {
		info["document_id"] = *doc.DocumentId
	}
	if doc.Title != nil {
		info["title"] = *doc.Title
	}
	if doc.RevisionId != nil {
		info["revision_id"] = *doc.RevisionId
	}

	out, _ := json.MarshalIndent(info, "", "  ")
	return string(out), nil
}

func (t *FeishuDocsTool) listBlocks(ctx context.Context, params map[string]any) (string, error) {
	docID, _ := params["document_id"].(string)
	if docID == "" {
		return "", fmt.Errorf("document_id is required")
	}

	req := larkdocx.NewListDocumentBlockReqBuilder().DocumentId(docID).PageSize(500).Build()
	resp, err := t.Client.Docx.DocumentBlock.List(ctx, req)
	if err != nil {
		return "", fmt.Errorf("list blocks failed: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("list blocks failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	type blockInfo struct {
		BlockID   string `json:"block_id"`
		BlockType int    `json:"block_type"`
		ParentID  string `json:"parent_id,omitempty"`
		Children  int    `json:"children_count"`
	}

	var blocks []blockInfo
	for _, b := range resp.Data.Items {
		bi := blockInfo{}
		if b.BlockId != nil {
			bi.BlockID = *b.BlockId
		}
		if b.BlockType != nil {
			bi.BlockType = *b.BlockType
		}
		if b.ParentId != nil {
			bi.ParentID = *b.ParentId
		}
		bi.Children = len(b.Children)
		blocks = append(blocks, bi)
	}

	result := map[string]any{
		"total":  len(blocks),
		"blocks": blocks,
	}
	if resp.Data.HasMore != nil && *resp.Data.HasMore {
		result["has_more"] = true
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}
