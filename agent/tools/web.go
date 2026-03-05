package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	htmllib "html"
)

var userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_7_2) AppleWebKit/537.36"

// WebSearchTool 网页搜索 (对标 pp-claw/agent/tools/web.py:WebSearchTool)
type WebSearchTool struct {
	APIKey     string
	MaxResults int
}

func (t *WebSearchTool) Name() string { return "web_search" }
func (t *WebSearchTool) Description() string {
	return "Search the web. Returns titles, URLs, and snippets."
}
func (t *WebSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query"},
			"count": map[string]any{"type": "integer", "description": "Results (1-10)", "minimum": 1, "maximum": 10},
		},
		"required": []any{"query"},
	}
}

func (t *WebSearchTool) Execute(_ context.Context, params map[string]any) (string, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	apiKey := t.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("BRAVE_API_KEY")
	}
	if apiKey == "" {
		return "Error: Brave Search API key not configured. Set BRAVE_API_KEY env var or configure in pp-claw.yaml", nil
	}

	count := t.MaxResults
	if count <= 0 {
		count = 5
	}
	if c, ok := params["count"].(float64); ok && c > 0 {
		count = int(c)
		if count > 10 {
			count = 10
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	reqURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), count)

	req, _ := http.NewRequest("GET", reqURL, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse search results failed: %w", err)
	}

	if len(result.Web.Results) == 0 {
		return fmt.Sprintf("No results for: %s", query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Results for: %s\n\n", query))
	for i, item := range result.Web.Results {
		if i >= count {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, item.Title, item.URL))
		if item.Description != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", item.Description))
		}
	}
	return sb.String(), nil
}

// WebFetchTool 网页抓取 (对标 pp-claw/agent/tools/web.py:WebFetchTool)
type WebFetchTool struct {
	MaxChars int
}

func (t *WebFetchTool) Name() string { return "web_fetch" }
func (t *WebFetchTool) Description() string {
	return "Fetch URL and extract readable content (HTML → text)."
}
func (t *WebFetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":      map[string]any{"type": "string", "description": "URL to fetch"},
			"maxChars": map[string]any{"type": "integer", "description": "Max characters to return", "minimum": 100},
		},
		"required": []any{"url"},
	}
}

func (t *WebFetchTool) Execute(_ context.Context, params map[string]any) (string, error) {
	rawURL, _ := params["url"].(string)
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	// 验证 URL
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return jsonResult(map[string]any{"error": "Only http/https URLs allowed", "url": rawURL}), nil
	}

	maxChars := t.MaxChars
	if maxChars <= 0 {
		maxChars = 50000
	}
	if mc, ok := params["maxChars"].(float64); ok && mc > 0 {
		maxChars = int(mc)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, _ := http.NewRequest("GET", rawURL, nil)
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error(), "url": rawURL}), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxChars*2)))
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error(), "url": rawURL}), nil
	}

	text := string(body)
	contentType := resp.Header.Get("Content-Type")

	var extractor string
	if strings.Contains(contentType, "application/json") {
		extractor = "json"
		// 重新格式化 JSON
		var v any
		if json.Unmarshal(body, &v) == nil {
			if pretty, err := json.MarshalIndent(v, "", "  "); err == nil {
				text = string(pretty)
			}
		}
	} else if strings.Contains(contentType, "text/html") || strings.HasPrefix(strings.TrimSpace(strings.ToLower(text[:min(256, len(text))])), "<!doctype") {
		extractor = "text"
		text = htmlToText(text)
	} else {
		extractor = "raw"
	}

	truncated := len(text) > maxChars
	if truncated {
		text = text[:maxChars]
	}

	return jsonResult(map[string]any{
		"url":       rawURL,
		"finalUrl":  resp.Request.URL.String(),
		"status":    resp.StatusCode,
		"extractor": extractor,
		"truncated": truncated,
		"length":    len(text),
		"text":      text,
	}), nil
}

// 预编译 HTML 解析正则（包级变量，避免每次调用重复编译）
var (
	reScript  = regexp.MustCompile(`(?is)<script[\s\S]*?</script>`)
	reStyle   = regexp.MustCompile(`(?is)<style[\s\S]*?</style>`)
	reH1      = regexp.MustCompile(`(?is)<h1[^>]*>([\s\S]*?)</h1>`)
	reH2      = regexp.MustCompile(`(?is)<h2[^>]*>([\s\S]*?)</h2>`)
	reH3      = regexp.MustCompile(`(?is)<h3[^>]*>([\s\S]*?)</h3>`)
	reH4      = regexp.MustCompile(`(?is)<h4[^>]*>([\s\S]*?)</h4>`)
	reH5      = regexp.MustCompile(`(?is)<h5[^>]*>([\s\S]*?)</h5>`)
	reH6      = regexp.MustCompile(`(?is)<h6[^>]*>([\s\S]*?)</h6>`)
	reLi      = regexp.MustCompile(`(?is)<li[^>]*>([\s\S]*?)</li>`)
	reBlock   = regexp.MustCompile(`(?i)</(p|div|section|article)>`)
	reBr      = regexp.MustCompile(`(?i)<(br|hr)\s*/?>`)
	reSpaces  = regexp.MustCompile(`[ \t]+`)
	reLines   = regexp.MustCompile(`\n{3,}`)
	reStripTg = regexp.MustCompile(`<[^>]+>`)
)

// headingPatterns h1-h6 正则 + 对应 markdown 前缀
var headingPatterns = []struct {
	re     *regexp.Regexp
	prefix string
}{
	{reH1, "# "},
	{reH2, "## "},
	{reH3, "### "},
	{reH4, "#### "},
	{reH5, "##### "},
	{reH6, "###### "},
}

// htmlToText 简单的 HTML → 文本转换
func htmlToText(html string) string {
	// 移除 script/style
	html = reScript.ReplaceAllString(html, "")
	html = reStyle.ReplaceAllString(html, "")

	// 转换标题 h1-h6（逐级替换，无需反向引用）
	for _, hp := range headingPatterns {
		html = hp.re.ReplaceAllStringFunc(html, func(s string) string {
			m := hp.re.FindStringSubmatch(s)
			if len(m) >= 2 {
				return "\n" + hp.prefix + stripTags(m[1]) + "\n"
			}
			return s
		})
	}

	// 列表项
	html = reLi.ReplaceAllStringFunc(html, func(s string) string {
		m := reLi.FindStringSubmatch(s)
		if len(m) >= 2 {
			return "\n- " + stripTags(m[1])
		}
		return s
	})

	// 段落/div → 换行
	html = reBlock.ReplaceAllString(html, "\n\n")
	html = reBr.ReplaceAllString(html, "\n")

	// 移除所有标签
	text := stripTags(html)

	// 解码 HTML 实体
	text = htmllib.UnescapeString(text)

	// 规范化空白
	text = reSpaces.ReplaceAllString(text, " ")
	text = reLines.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// stripTags 移除 HTML 标签
func stripTags(s string) string {
	return reStripTg.ReplaceAllString(s, "")
}

func jsonResult(v map[string]any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
