package agent

import (
	"slices"

	"github.com/yangkun19921001/PP-Claw/config"
)

// AgentRouter 根据 binding 路由表将消息分发到目标 Agent。
// 4 层优先级，第一个匹配的 binding 胜出:
//
//	Tier 1: SenderIDs — binding.SenderIDs 包含 senderID
//	Tier 2: ChatIDs   — binding.ChatIDs 包含 chatID, 且 channel 匹配
//	Tier 3: Channel+Account — binding.Channel 和 binding.AccountID 匹配
//	Tier 4: Default   — binding.Default == true
//	Fallback: defaultAgentID
type AgentRouter struct {
	bindings       []config.BindingEntry
	defaultAgentID string
}

// NewAgentRouter 创建路由器。bindings 按配置顺序保存，Resolve 按优先级扫描。
func NewAgentRouter(agentsCfg *config.AgentsConfig) *AgentRouter {
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
	}
}

// Resolve 返回匹配的 agentID。
func (r *AgentRouter) Resolve(channel, accountID, chatID, senderID string) string {
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
		if b.Channel != "" && !b.Default && len(b.ChatIDs) == 0 && len(b.SenderIDs) == 0 {
			if b.Channel == channel {
				if b.AccountID == "" || b.AccountID == accountID {
					return b.AgentID
				}
			}
		}
	}

	// Tier 4: Default binding
	for i := range r.bindings {
		b := &r.bindings[i]
		if b.Default {
			return b.AgentID
		}
	}

	return r.defaultAgentID
}

