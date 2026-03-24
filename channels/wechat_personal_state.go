package channels

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var nonFileNameRE = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeAccountID(accountID string) string {
	if accountID == "" {
		return "default"
	}
	return nonFileNameRE.ReplaceAllString(accountID, "_")
}

func (w *WechatPersonalChannel) ensureStateDirs() error {
	dirs := []string{
		w.stateDir,
		filepath.Join(w.stateDir, "accounts"),
		filepath.Join(w.stateDir, "media"),
		filepath.Join(w.stateDir, "media", "inbound"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

func (w *WechatPersonalChannel) accountStatePath(accountID string) string {
	return filepath.Join(w.stateDir, "accounts", sanitizeAccountID(accountID)+".json")
}

func (w *WechatPersonalChannel) inboundMediaDir() string {
	return filepath.Join(w.stateDir, "media", "inbound")
}

func (w *WechatPersonalChannel) loadPersistedStates() (map[string]*wechatPersistedAccountState, error) {
	if err := w.ensureStateDirs(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(w.stateDir, "accounts"))
	if err != nil {
		return nil, err
	}
	states := make(map[string]*wechatPersistedAccountState)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(w.stateDir, "accounts", entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var state wechatPersistedAccountState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, fmt.Errorf("parse wechat account state %s: %w", path, err)
		}
		if state.AccountID == "" {
			continue
		}
		states[state.AccountID] = &state
	}
	return states, nil
}

func (w *WechatPersonalChannel) saveAccountState(account *wechatAccountRuntime) error {
	if account == nil {
		return nil
	}
	if err := w.ensureStateDirs(); err != nil {
		return err
	}
	account.mu.RLock()
	state := wechatPersistedAccountState{
		AccountID:     account.id,
		Token:         account.token,
		ILinkUserID:   account.ilinkUserID,
		ILinkBotID:    account.ilinkBotID,
		BaseURL:       account.baseURL,
		CDNBaseURL:    account.cdnBaseURL,
		BotType:       account.botType,
		GetUpdatesBuf: account.getUpdatesBuf,
		PausedUntil:   account.pausedUntil,
		LastError:     account.lastError,
		LastSeenAt:    account.lastSeenAt,
		LastMessageAt: account.lastMessageAt,
		TypingTicket:  account.typingTicket,
		UpdatedAt:     time.Now(),
	}
	account.mu.RUnlock()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(w.accountStatePath(account.id), data, 0644)
}

func (w *WechatPersonalChannel) listAccountIDs() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	ids := make([]string, 0, len(w.accounts))
	for id := range w.accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
