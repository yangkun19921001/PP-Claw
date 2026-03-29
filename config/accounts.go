package config

import (
	"reflect"
	"sort"
)

// mergeNonZero 将 src 中的非零值字段覆盖到 dst 中（dst/src 必须是同类型 struct 指针）
func mergeNonZero(dst, src any) {
	dv := reflect.ValueOf(dst).Elem()
	sv := reflect.ValueOf(src).Elem()
	for i := 0; i < dv.NumField(); i++ {
		sf := sv.Field(i)
		if !sf.IsZero() {
			dv.Field(i).Set(sf)
		}
	}
}

// defaultAccountID 通用逻辑：优先 defaultAccount，然后 "default"，否则 keys 字母序第一个
func defaultAccountID(defaultAccount string, keys []string) string {
	if defaultAccount != "" {
		return defaultAccount
	}
	if len(keys) == 0 {
		return "default"
	}
	sort.Strings(keys)
	return keys[0]
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// --- Telegram ---

func (c *TelegramConfig) ResolveAccounts() map[string]TelegramAccountConfig {
	if len(c.Accounts) == 0 {
		return map[string]TelegramAccountConfig{"default": c.TelegramAccountConfig}
	}
	result := make(map[string]TelegramAccountConfig, len(c.Accounts))
	for id, override := range c.Accounts {
		merged := c.TelegramAccountConfig
		mergeNonZero(&merged, &override)
		result[id] = merged
	}
	return result
}

func (c *TelegramConfig) DefaultAccountID() string {
	return defaultAccountID(c.DefaultAccount, mapKeys(c.Accounts))
}

// --- Discord ---

func (c *DiscordConfig) ResolveAccounts() map[string]DiscordAccountConfig {
	if len(c.Accounts) == 0 {
		return map[string]DiscordAccountConfig{"default": c.DiscordAccountConfig}
	}
	result := make(map[string]DiscordAccountConfig, len(c.Accounts))
	for id, override := range c.Accounts {
		merged := c.DiscordAccountConfig
		mergeNonZero(&merged, &override)
		result[id] = merged
	}
	return result
}

func (c *DiscordConfig) DefaultAccountID() string {
	return defaultAccountID(c.DefaultAccount, mapKeys(c.Accounts))
}

// --- Slack ---

func (c *SlackConfig) ResolveAccounts() map[string]SlackAccountConfig {
	if len(c.Accounts) == 0 {
		return map[string]SlackAccountConfig{"default": c.SlackAccountConfig}
	}
	result := make(map[string]SlackAccountConfig, len(c.Accounts))
	for id, override := range c.Accounts {
		merged := c.SlackAccountConfig
		mergeNonZero(&merged, &override)
		result[id] = merged
	}
	return result
}

func (c *SlackConfig) DefaultAccountID() string {
	return defaultAccountID(c.DefaultAccount, mapKeys(c.Accounts))
}

// --- WhatsApp ---

func (c *WhatsAppConfig) ResolveAccounts() map[string]WhatsAppAccountConfig {
	if len(c.Accounts) == 0 {
		return map[string]WhatsAppAccountConfig{"default": c.WhatsAppAccountConfig}
	}
	result := make(map[string]WhatsAppAccountConfig, len(c.Accounts))
	for id, override := range c.Accounts {
		merged := c.WhatsAppAccountConfig
		mergeNonZero(&merged, &override)
		result[id] = merged
	}
	return result
}

func (c *WhatsAppConfig) DefaultAccountID() string {
	return defaultAccountID(c.DefaultAccount, mapKeys(c.Accounts))
}

// --- Feishu ---

func (c *FeishuConfig) ResolveAccounts() map[string]FeishuAccountConfig {
	if len(c.Accounts) == 0 {
		return map[string]FeishuAccountConfig{"default": c.FeishuAccountConfig}
	}
	result := make(map[string]FeishuAccountConfig, len(c.Accounts))
	for id, override := range c.Accounts {
		merged := c.FeishuAccountConfig
		mergeNonZero(&merged, &override)
		result[id] = merged
	}
	return result
}

func (c *FeishuConfig) DefaultAccountID() string {
	return defaultAccountID(c.DefaultAccount, mapKeys(c.Accounts))
}

// --- DingTalk ---

func (c *DingTalkConfig) ResolveAccounts() map[string]DingTalkAccountConfig {
	if len(c.Accounts) == 0 {
		return map[string]DingTalkAccountConfig{"default": c.DingTalkAccountConfig}
	}
	result := make(map[string]DingTalkAccountConfig, len(c.Accounts))
	for id, override := range c.Accounts {
		merged := c.DingTalkAccountConfig
		mergeNonZero(&merged, &override)
		result[id] = merged
	}
	return result
}

func (c *DingTalkConfig) DefaultAccountID() string {
	return defaultAccountID(c.DefaultAccount, mapKeys(c.Accounts))
}

// --- Email ---

func (c *EmailConfig) ResolveAccounts() map[string]EmailAccountConfig {
	if len(c.Accounts) == 0 {
		return map[string]EmailAccountConfig{"default": c.EmailAccountConfig}
	}
	result := make(map[string]EmailAccountConfig, len(c.Accounts))
	for id, override := range c.Accounts {
		merged := c.EmailAccountConfig
		mergeNonZero(&merged, &override)
		result[id] = merged
	}
	return result
}

func (c *EmailConfig) DefaultAccountID() string {
	return defaultAccountID(c.DefaultAccount, mapKeys(c.Accounts))
}

// --- QQ ---

func (c *QQConfig) ResolveAccounts() map[string]QQAccountConfig {
	if len(c.Accounts) == 0 {
		return map[string]QQAccountConfig{"default": c.QQAccountConfig}
	}
	result := make(map[string]QQAccountConfig, len(c.Accounts))
	for id, override := range c.Accounts {
		merged := c.QQAccountConfig
		mergeNonZero(&merged, &override)
		result[id] = merged
	}
	return result
}

func (c *QQConfig) DefaultAccountID() string {
	return defaultAccountID(c.DefaultAccount, mapKeys(c.Accounts))
}

// --- Mochat ---

func (c *MochatConfig) ResolveAccounts() map[string]MochatAccountConfig {
	if len(c.Accounts) == 0 {
		return map[string]MochatAccountConfig{"default": c.MochatAccountConfig}
	}
	result := make(map[string]MochatAccountConfig, len(c.Accounts))
	for id, override := range c.Accounts {
		merged := c.MochatAccountConfig
		mergeNonZero(&merged, &override)
		result[id] = merged
	}
	return result
}

func (c *MochatConfig) DefaultAccountID() string {
	return defaultAccountID(c.DefaultAccount, mapKeys(c.Accounts))
}

// --- Wecom ---

func (c *WecomConfig) ResolveAccounts() map[string]WecomAccountConfig {
	if len(c.Accounts) == 0 {
		return map[string]WecomAccountConfig{"default": c.WecomAccountConfig}
	}
	result := make(map[string]WecomAccountConfig, len(c.Accounts))
	for id, override := range c.Accounts {
		merged := c.WecomAccountConfig
		mergeNonZero(&merged, &override)
		result[id] = merged
	}
	return result
}

func (c *WecomConfig) DefaultAccountID() string {
	return defaultAccountID(c.DefaultAccount, mapKeys(c.Accounts))
}

// --- Matrix ---

func (c *MatrixConfig) ResolveAccounts() map[string]MatrixAccountConfig {
	if len(c.Accounts) == 0 {
		return map[string]MatrixAccountConfig{"default": c.MatrixAccountConfig}
	}
	result := make(map[string]MatrixAccountConfig, len(c.Accounts))
	for id, override := range c.Accounts {
		merged := c.MatrixAccountConfig
		mergeNonZero(&merged, &override)
		result[id] = merged
	}
	return result
}

func (c *MatrixConfig) DefaultAccountID() string {
	return defaultAccountID(c.DefaultAccount, mapKeys(c.Accounts))
}
