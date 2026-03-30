package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/yangkun19921001/PP-Claw/config"
	"go.uber.org/zap"
)

// ContentMatcher 内容感知匹配器 — keyword/regex 快速匹配 + LLM 智能分类。
// 每个实例对应一条 BindingEntry 的 ContentMatch 配置。
type ContentMatcher struct {
	agentID    string
	keywords   []string         // 小写化后的关键词列表
	regex      *regexp.Regexp   // 预编译正则（可为 nil）
	llmRoute   bool
	llmPrompt  string
	candidates []string
}

// ContentRouter 内容感知路由层，独立于静态路由。
// 职责：根据消息内容匹配 binding 中的 content_match 规则。
type ContentRouter struct {
	keywordMatchers []*ContentMatcher // keyword 匹配器（Tier 4）
	regexMatchers   []*ContentMatcher // regex 匹配器（Tier 5）
	llmMatchers     []*ContentMatcher // LLM 匹配器（Tier 6）
	classifier      *LLMClassifier    // LLM 分类器（可为 nil）
	logger          *zap.Logger
}

// NewContentRouter 从 bindings 中提取 content_match 规则，构建内容路由器。
func NewContentRouter(bindings []config.BindingEntry, logger *zap.Logger) *ContentRouter {
	if logger == nil {
		logger = zap.NewNop()
	}
	cr := &ContentRouter{logger: logger}

	for _, b := range bindings {
		if b.ContentMatch == nil {
			continue
		}
		cm := b.ContentMatch
		matcher := &ContentMatcher{
			agentID:    b.AgentID,
			candidates: cm.Candidates,
			llmRoute:   cm.LLMRoute,
			llmPrompt:  cm.LLMPrompt,
		}

		// 预处理 keywords（小写化）
		if len(cm.Keywords) > 0 {
			matcher.keywords = make([]string, len(cm.Keywords))
			for i, kw := range cm.Keywords {
				matcher.keywords[i] = strings.ToLower(kw)
			}
			cr.keywordMatchers = append(cr.keywordMatchers, matcher)
		}

		// 预编译正则
		if cm.Regex != "" {
			re, err := regexp.Compile(cm.Regex)
			if err != nil {
				logger.Warn("content_match regex 编译失败",
					zap.String("agent_id", b.AgentID),
					zap.String("regex", cm.Regex),
					zap.Error(err),
				)
			} else {
				matcher.regex = re
				cr.regexMatchers = append(cr.regexMatchers, matcher)
			}
		}

		// LLM 路由
		if cm.LLMRoute {
			cr.llmMatchers = append(cr.llmMatchers, matcher)
		}
	}

	return cr
}

// SetLLMModel 设置 LLM 分类器使用的模型（懒初始化，仅有 llm_route binding 时需要）。
func (cr *ContentRouter) SetLLMModel(model einomodel.ToolCallingChatModel) {
	if len(cr.llmMatchers) > 0 && model != nil {
		cr.classifier = NewLLMClassifier(model, cr.logger)
	}
}

// HasContentRules 是否有任何内容匹配规则。
func (cr *ContentRouter) HasContentRules() bool {
	return len(cr.keywordMatchers) > 0 || len(cr.regexMatchers) > 0 || len(cr.llmMatchers) > 0
}

// Match 根据消息内容匹配 agent ID。按优先级：keyword → regex → LLM。
// 无匹配返回空字符串。
func (cr *ContentRouter) Match(ctx context.Context, content string) string {
	if content == "" {
		return ""
	}

	// Tier 4: Keyword（亚微秒级）
	lowerContent := strings.ToLower(content)
	for _, m := range cr.keywordMatchers {
		for _, kw := range m.keywords {
			if strings.Contains(lowerContent, kw) {
				return m.agentID
			}
		}
	}

	// Tier 5: Regex（微秒级）
	for _, m := range cr.regexMatchers {
		if m.regex != nil && m.regex.MatchString(content) {
			return m.agentID
		}
	}

	// Tier 6: LLM（毫秒级，带缓存）
	if cr.classifier != nil {
		for _, m := range cr.llmMatchers {
			if id := cr.classifier.Classify(ctx, content, m.candidates, m.llmPrompt); id != "" {
				return id
			}
		}
	}

	return ""
}

// ============ LLM Classifier ============

// LLMClassifier 使用 LLM 对消息内容进行智能分类路由。
// 内置 LRU 缓存避免重复调用。
type LLMClassifier struct {
	model  einomodel.ToolCallingChatModel
	cache  map[string]cacheEntry
	mu     sync.RWMutex
	maxAge time.Duration
	logger *zap.Logger
}

type cacheEntry struct {
	agentID   string
	createdAt time.Time
}

// NewLLMClassifier 创建 LLM 分类器。
func NewLLMClassifier(model einomodel.ToolCallingChatModel, logger *zap.Logger) *LLMClassifier {
	return &LLMClassifier{
		model:  model,
		cache:  make(map[string]cacheEntry),
		maxAge: 5 * time.Minute,
		logger: logger,
	}
}

// Classify 使用 LLM 分类消息内容到一个候选 agent ID。
// 结果缓存 5 分钟。无法匹配返回空字符串。
func (c *LLMClassifier) Classify(ctx context.Context, content string, candidates []string, customPrompt string) string {
	if len(candidates) == 0 || c.model == nil {
		return ""
	}

	// 查缓存
	key := c.cacheKey(content, candidates)
	if id := c.getFromCache(key); id != "" {
		return id
	}

	// 构建分类 prompt
	prompt := customPrompt
	if prompt == "" {
		prompt = fmt.Sprintf(
			"You are a message router. Given the following agent candidates: [%s], "+
				"classify this message and reply with ONLY the agent ID that should handle it. "+
				"Reply with just the ID, nothing else.",
			strings.Join(candidates, ", "),
		)
	}

	messages := []*schema.Message{
		{Role: schema.System, Content: prompt},
		{Role: schema.User, Content: content},
	}

	resp, err := c.model.Generate(ctx, messages)
	if err != nil {
		c.logger.Warn("LLM 路由分类失败", zap.Error(err))
		return ""
	}

	result := strings.TrimSpace(resp.Content)

	// 校验结果是否在候选列表中
	candidateSet := make(map[string]bool, len(candidates))
	for _, cand := range candidates {
		candidateSet[cand] = true
	}
	if !candidateSet[result] {
		// 尝试模糊匹配（LLM 可能返回带引号或多余内容）
		for _, cand := range candidates {
			if strings.Contains(strings.ToLower(result), strings.ToLower(cand)) {
				result = cand
				break
			}
		}
		if !candidateSet[result] {
			c.logger.Warn("LLM 路由返回了无效的 agent ID",
				zap.String("result", result),
				zap.Strings("candidates", candidates),
			)
			return ""
		}
	}

	// 写入缓存
	c.putToCache(key, result)
	return result
}

func (c *LLMClassifier) cacheKey(content string, candidates []string) string {
	h := sha256.Sum256([]byte(content + "|" + strings.Join(candidates, ",")))
	return hex.EncodeToString(h[:8])
}

func (c *LLMClassifier) getFromCache(key string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.cache[key]
	if !ok || time.Since(entry.createdAt) > c.maxAge {
		return ""
	}
	return entry.agentID
}

func (c *LLMClassifier) putToCache(key, agentID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 简单淘汰：超过 1000 条时清空
	if len(c.cache) > 1000 {
		c.cache = make(map[string]cacheEntry)
	}
	c.cache[key] = cacheEntry{agentID: agentID, createdAt: time.Now()}
}
