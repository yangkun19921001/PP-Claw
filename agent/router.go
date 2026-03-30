package agent

import (
	"context"
	"slices"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/yangkun19921001/PP-Claw/config"
	"go.uber.org/zap"
)

// AgentRouter 根据 binding 路由表将消息分发到目标 Agent。
// 7 层优先级，第一个匹配的 binding 胜出:
//
//	Tier 1: SenderIDs      — binding.SenderIDs 包含 senderID
//	Tier 2: ChatIDs        — binding.ChatIDs 包含 chatID, 且 channel 匹配
//	Tier 3: Channel+Account — binding.Channel 和 binding.AccountID 匹配
//	Tier 4: Content Keywords — content_match.keywords 子串命中
//	Tier 5: Content Regex   — content_match.regex 匹配
//	Tier 6: Content LLM     — content_match.llm_route 智能分类
//	Tier 7: Default         — binding.Default == true
//	Fallback: defaultAgentID
type AgentRouter struct {
	bindings       []config.BindingEntry
	defaultAgentID string
	contentRouter  *ContentRouter
}

// NewAgentRouter 创建路由器。bindings 按配置顺序保存，Resolve 按优先级扫描。
func NewAgentRouter(agentsCfg *config.AgentsConfig, logger *zap.Logger) *AgentRouter {
	if logger == nil {
		logger = zap.NewNop()
	}
	defaultID := "default"
	if len(agentsCfg.List) > 0 {
		for _, a := range agentsCfg.List {
			if a.Default {
				defaultID = a.ID
				break
			}
		}
		if defaultID == "default" {
			defaultID = agentsCfg.List[0].ID
		}
	}
	return &AgentRouter{
		bindings:       agentsCfg.Bindings,
		defaultAgentID: defaultID,
		contentRouter:  NewContentRouter(agentsCfg.Bindings, logger),
	}
}

// SetLLMModel 设置内容路由 LLM 分类器的模型。
func (r *AgentRouter) SetLLMModel(model einomodel.ToolCallingChatModel) {
	r.contentRouter.SetLLMModel(model)
}

// Resolve 仅基于元数据的静态路由（向后兼容）。
func (r *AgentRouter) Resolve(channel, accountID, chatID, senderID string) string {
	return r.ResolveWithContent(context.Background(), channel, accountID, chatID, senderID, "")
}

// ResolveWithContent 完整路由：静态元数据 + 内容感知匹配。
func (r *AgentRouter) ResolveWithContent(ctx context.Context, channel, accountID, chatID, senderID, content string) string {
	// Tier 1: SenderIDs
	for i := range r.bindings {
		b := &r.bindings[i]
		if len(b.SenderIDs) > 0 && slices.Contains(b.SenderIDs, senderID) {
			return b.AgentID
		}
	}

	// Tier 2: ChatIDs (channel 须匹配或为空)
	for i := range r.bindings {
		b := &r.bindings[i]
		if len(b.ChatIDs) > 0 && slices.Contains(b.ChatIDs, chatID) {
			if b.Channel == "" || b.Channel == channel {
				return b.AgentID
			}
		}
	}

	// Tier 3: Channel + AccountID
	for i := range r.bindings {
		b := &r.bindings[i]
		if b.Channel != "" && !b.Default && len(b.ChatIDs) == 0 && len(b.SenderIDs) == 0 && b.ContentMatch == nil {
			if b.Channel == channel {
				if b.AccountID == "" || b.AccountID == accountID {
					return b.AgentID
				}
			}
		}
	}

	// Tier 4-6: Content 匹配（keyword → regex → LLM）
	if content != "" && r.contentRouter.HasContentRules() {
		if id := r.contentRouter.Match(ctx, content); id != "" {
			return id
		}
	}

	// Tier 7: Default binding
	for i := range r.bindings {
		b := &r.bindings[i]
		if b.Default {
			return b.AgentID
		}
	}

	return r.defaultAgentID
}
