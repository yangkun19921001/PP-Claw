//go:build amd64 || arm64 || riscv64 || mips64 || mips64le || ppc64

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
	larksearch "github.com/larksuite/oapi-sdk-go/v3/service/search/v2"
	larkwiki "github.com/larksuite/oapi-sdk-go/v3/service/wiki/v2"
)

func init() {
	RegisterFeishuToolFactory(func(cfg *FeishuToolsConfig) Tool {
		client := lark.NewClient(cfg.AppID, cfg.AppSecret)

		tool := &FeishuWikiTool{
			Client:           client,
			OAuthRedirectURL: cfg.OAuthRedirectURL,
			SearchMaxResults: cfg.SearchMaxResults,
		}

		// 配置了 OAuth 才启用搜索的用户授权流程
		if cfg.OAuthRedirectURL != "" {
			tool.TokenManager = NewFeishuTokenManager(&FeishuTokenManagerConfig{
				Client: client,
				AppID:  cfg.AppID,
				Logger: cfg.Logger,
			})
		}

		return tool
	})
}

// FeishuWikiTool 飞书知识库工具
type FeishuWikiTool struct {
	Client           *lark.Client
	TokenManager     *FeishuTokenManager
	OAuthRedirectURL string
	SearchMaxResults int
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
		return t.search(ctx, params)
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
		content, err := t.readDocContent(ctx, objToken)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("# %s\n\n%s", title, content), nil
	}

	return fmt.Sprintf("Node: %s (type: %s, obj_token: %s)\nThis node type does not support direct content reading.", title, objType, objToken), nil
}

// search 搜索飞书文档和知识库（需要 user_access_token）
func (t *FeishuWikiTool) search(ctx context.Context, params map[string]any) (string, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required for search")
	}

	// 检查 token manager
	if t.TokenManager == nil {
		return "Search is not available: OAuth not configured. Please set feishu.oauth_port and feishu.oauth_redirect_url in config.", nil
	}

	// 获取 user token
	userToken, err := t.TokenManager.GetUserToken(ctx)
	if err != nil {
		// 没有 token，返回授权链接
		authURL := t.TokenManager.GetAuthURL(t.OAuthRedirectURL)
		return fmt.Sprintf("需要飞书用户授权才能使用搜索功能。请点击以下链接完成授权，授权后重新搜索即可：\n\n%s", authURL), nil
	}

	// 调用搜索 API
	maxResults := t.SearchMaxResults
	if maxResults <= 0 {
		maxResults = 3
	}

	pageSize := 20
	if ps, ok := params["page_size"].(float64); ok && ps > 0 {
		pageSize = int(ps)
	}

	searchBody := larksearch.NewSearchDocWikiReqBodyBuilder().
		Query(query).
		PageSize(pageSize).
		Build()

	searchReq := larksearch.NewSearchDocWikiReqBuilder().
		Body(searchBody).
		Build()

	resp, err := t.Client.Search.DocWiki.Search(ctx, searchReq,
		larkcore.WithUserAccessToken(userToken))
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}
	if !resp.Success() {
		// token 可能已失效
		if resp.Code == 99991668 || resp.Code == 99991672 || resp.Code == 99991679 {
			authURL := t.TokenManager.GetAuthURL(t.OAuthRedirectURL)
			return fmt.Sprintf("用户授权已失效或权限不足，请重新授权：\n\n%s", authURL), nil
		}
		return "", fmt.Errorf("search failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	if resp.Data == nil || len(resp.Data.ResUnits) == 0 {
		return fmt.Sprintf("未找到与 \"%s\" 相关的文档。", query), nil
	}

	// 构建搜索结果，自动读取前 N 个文档内容
	var sb strings.Builder
	total := 0
	if resp.Data.Total != nil {
		total = *resp.Data.Total
	}
	sb.WriteString(fmt.Sprintf("搜索 \"%s\" 共找到 %d 条结果：\n\n", query, total))

	readCount := 0
	for i, unit := range resp.Data.ResUnits {
		title := ""
		if unit.TitleHighlighted != nil {
			title = *unit.TitleHighlighted
		}
		summary := ""
		if unit.SummaryHighlighted != nil {
			summary = *unit.SummaryHighlighted
		}
		entityType := ""
		if unit.EntityType != nil {
			entityType = *unit.EntityType
		}

		docToken := ""
		docType := ""
		docURL := ""
		if unit.ResultMeta != nil {
			if unit.ResultMeta.Token != nil {
				docToken = *unit.ResultMeta.Token
			}
			if unit.ResultMeta.DocTypes != nil {
				docType = *unit.ResultMeta.DocTypes
			}
			if unit.ResultMeta.Url != nil {
				docURL = *unit.ResultMeta.Url
			}
		}

		sb.WriteString(fmt.Sprintf("---\n### %d. %s\n", i+1, title))
		sb.WriteString(fmt.Sprintf("类型: %s (%s) | 链接: %s\n", entityType, docType, docURL))
		if summary != "" {
			sb.WriteString(fmt.Sprintf("摘要: %s\n", summary))
		}

		// 自动读取前 N 个 docx 文档的正文
		if readCount < maxResults && docToken != "" && docType == "docx" {
			content, err := t.readDocContent(ctx, docToken)
			if err == nil && content != "" {
				sb.WriteString(fmt.Sprintf("\n**正文内容：**\n%s\n", content))
				readCount++
			}
		}
		sb.WriteString("\n")
	}

	if resp.Data.HasMore != nil && *resp.Data.HasMore {
		sb.WriteString("（还有更多结果，可通过 page_token 翻页）\n")
	}

	return sb.String(), nil
}

// readDocContent 读取文档原始内容
func (t *FeishuWikiTool) readDocContent(ctx context.Context, docID string) (string, error) {
	docReq := larkdocx.NewRawContentDocumentReqBuilder().DocumentId(docID).Lang(0).Build()
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

	const maxLen = 30000
	if len(content) > maxLen {
		content = content[:maxLen] + "\n\n... (content truncated)"
	}

	return content, nil
}
