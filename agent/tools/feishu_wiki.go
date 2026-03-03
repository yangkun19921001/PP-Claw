//go:build amd64 || arm64 || riscv64 || mips64 || ppc64

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
	larkwiki "github.com/larksuite/oapi-sdk-go/v3/service/wiki/v2"
)

func init() {
	RegisterFeishuToolFactory(func(appID, appSecret string) Tool {
		return &FeishuWikiTool{Client: lark.NewClient(appID, appSecret)}
	})
}

// FeishuWikiTool 飞书知识库工具
type FeishuWikiTool struct {
	Client *lark.Client
}

func (t *FeishuWikiTool) Name() string { return "feishu_wiki" }

func (t *FeishuWikiTool) Description() string {
	return "Access Feishu/Lark wiki knowledge base. Actions: list_spaces, list_nodes, read_node, search."
}

func (t *FeishuWikiTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform: list_spaces, list_nodes, read_node, search",
				"enum":        []any{"list_spaces", "list_nodes", "read_node", "search"},
			},
			"space_id": map[string]any{
				"type":        "string",
				"description": "Wiki space ID (for list_nodes)",
			},
			"node_token": map[string]any{
				"type":        "string",
				"description": "Node token (for read_node)",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Search query (for search)",
			},
			"page_size": map[string]any{
				"type":        "integer",
				"description": "Number of items per page (default 20)",
			},
			"page_token": map[string]any{
				"type":        "string",
				"description": "Pagination token for next page",
			},
		},
		"required": []any{"action"},
	}
}

func (t *FeishuWikiTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	action, _ := params["action"].(string)
	switch action {
	case "list_spaces":
		return t.listSpaces(ctx, params)
	case "list_nodes":
		return t.listNodes(ctx, params)
	case "read_node":
		return t.readNode(ctx, params)
	case "search":
		return t.search(params)
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

func (t *FeishuWikiTool) listSpaces(ctx context.Context, params map[string]any) (string, error) {
	pageSize := 20
	if ps, ok := params["page_size"].(float64); ok && ps > 0 {
		pageSize = int(ps)
	}

	builder := larkwiki.NewListSpaceReqBuilder().PageSize(pageSize)
	if pt, ok := params["page_token"].(string); ok && pt != "" {
		builder.PageToken(pt)
	}

	resp, err := t.Client.Wiki.Space.List(ctx, builder.Build())
	if err != nil {
		return "", fmt.Errorf("list spaces failed: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("list spaces failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	type spaceInfo struct {
		SpaceID     string `json:"space_id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}

	var spaces []spaceInfo
	for _, s := range resp.Data.Items {
		si := spaceInfo{}
		if s.SpaceId != nil {
			si.SpaceID = *s.SpaceId
		}
		if s.Name != nil {
			si.Name = *s.Name
		}
		if s.Description != nil {
			si.Description = *s.Description
		}
		spaces = append(spaces, si)
	}

	result := map[string]any{
		"spaces": spaces,
	}
	if resp.Data.HasMore != nil && *resp.Data.HasMore {
		result["has_more"] = true
		if resp.Data.PageToken != nil {
			result["page_token"] = *resp.Data.PageToken
		}
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func (t *FeishuWikiTool) listNodes(ctx context.Context, params map[string]any) (string, error) {
	spaceID, _ := params["space_id"].(string)
	if spaceID == "" {
		return "", fmt.Errorf("space_id is required for list_nodes")
	}

	pageSize := 20
	if ps, ok := params["page_size"].(float64); ok && ps > 0 {
		pageSize = int(ps)
	}

	builder := larkwiki.NewListSpaceNodeReqBuilder().SpaceId(spaceID).PageSize(pageSize)
	if pt, ok := params["page_token"].(string); ok && pt != "" {
		builder.PageToken(pt)
	}

	resp, err := t.Client.Wiki.SpaceNode.List(ctx, builder.Build())
	if err != nil {
		return "", fmt.Errorf("list nodes failed: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("list nodes failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	type nodeInfo struct {
		NodeToken string `json:"node_token"`
		ObjToken  string `json:"obj_token,omitempty"`
		ObjType   string `json:"obj_type,omitempty"`
		Title     string `json:"title"`
		HasChild  bool   `json:"has_child"`
	}

	var nodes []nodeInfo
	for _, n := range resp.Data.Items {
		ni := nodeInfo{}
		if n.NodeToken != nil {
			ni.NodeToken = *n.NodeToken
		}
		if n.ObjToken != nil {
			ni.ObjToken = *n.ObjToken
		}
		if n.ObjType != nil {
			ni.ObjType = *n.ObjType
		}
		if n.Title != nil {
			ni.Title = *n.Title
		}
		if n.HasChild != nil {
			ni.HasChild = *n.HasChild
		}
		nodes = append(nodes, ni)
	}

	result := map[string]any{
		"nodes": nodes,
	}
	if resp.Data.HasMore != nil && *resp.Data.HasMore {
		result["has_more"] = true
		if resp.Data.PageToken != nil {
			result["page_token"] = *resp.Data.PageToken
		}
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func (t *FeishuWikiTool) readNode(ctx context.Context, params map[string]any) (string, error) {
	nodeToken, _ := params["node_token"].(string)
	if nodeToken == "" {
		return "", fmt.Errorf("node_token is required for read_node")
	}

	// 获取节点信息
	nodeReq := larkwiki.NewGetNodeSpaceReqBuilder().Token(nodeToken).Build()
	nodeResp, err := t.Client.Wiki.Space.GetNode(ctx, nodeReq)
	if err != nil {
		return "", fmt.Errorf("get node failed: %w", err)
	}
	if !nodeResp.Success() {
		return "", fmt.Errorf("get node failed: code=%d msg=%s", nodeResp.Code, nodeResp.Msg)
	}

	node := nodeResp.Data.Node
	title := ""
	if node.Title != nil {
		title = *node.Title
	}

	objType := ""
	if node.ObjType != nil {
		objType = *node.ObjType
	}

	objToken := ""
	if node.ObjToken != nil {
		objToken = *node.ObjToken
	}

	// 读取文档内容（仅 docx 类型）
	if objType == "docx" && objToken != "" {
		docReq := larkdocx.NewRawContentDocumentReqBuilder().DocumentId(objToken).Lang(0).Build()
		docResp, err := t.Client.Docx.Document.RawContent(ctx, docReq)
		if err != nil {
			return "", fmt.Errorf("read document failed: %w", err)
		}
		if !docResp.Success() {
			return "", fmt.Errorf("read document failed: code=%d msg=%s", docResp.Code, docResp.Msg)
		}

		content := ""
		if docResp.Data.Content != nil {
			content = *docResp.Data.Content
		}

		// 截断过长内容
		const maxLen = 30000
		if len(content) > maxLen {
			content = content[:maxLen] + "\n\n... (content truncated)"
		}

		return fmt.Sprintf("# %s\n\n%s", title, content), nil
	}

	return fmt.Sprintf("Node: %s (type: %s, obj_token: %s)\nThis node type does not support direct content reading.", title, objType, objToken), nil
}

func (t *FeishuWikiTool) search(params map[string]any) (string, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required for search")
	}
	return fmt.Sprintf("Wiki search is not directly supported via the current API. To find content, use list_spaces to get available spaces, then list_nodes to browse nodes in a space, and read_node to read specific documents. Query: %s", query), nil
}
