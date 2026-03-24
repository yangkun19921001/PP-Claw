package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/config"
	"go.uber.org/zap"
)

type wechatAccountRuntime struct {
	id            string
	baseURL       string
	cdnBaseURL    string
	botType       string
	allowFrom     []string
	token         string
	ilinkUserID   string
	ilinkBotID    string
	getUpdatesBuf string
	typingTicket  string
	pausedUntil   time.Time
	lastError     string
	lastSeenAt    time.Time
	lastMessageAt time.Time
	running       bool
	cancel        context.CancelFunc
	pollTimeoutMS int
	mu            sync.RWMutex
}

// WechatPersonalChannel 个人微信 ClawBot 风格 channel
type WechatPersonalChannel struct {
	BaseChannel

	mu                  sync.RWMutex
	workspace           string
	stateDir            string
	baseURL             string
	cdnBaseURL          string
	botType             string
	pollTimeoutMS       int
	loginTimeoutS       int
	sessionPauseMinutes int
	requestTimeoutMS    int
	configTimeoutMS     int

	httpClient    *http.Client
	accounts      map[string]*wechatAccountRuntime
	logins        map[string]*wechatLoginSession
	contextTokens map[string]string
	started       bool
	rootCtx       context.Context
	rootCancel    context.CancelFunc
}

func init() {
	RegisterFactory("wechat_personal", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &WechatPersonalChannel{
			BaseChannel: BaseChannel{
				ChannelName: "wechat_personal",
				Bus:         msgBus,
				Logger:      logger,
			},
			httpClient:    &http.Client{Timeout: 60 * time.Second},
			accounts:      make(map[string]*wechatAccountRuntime),
			logins:        make(map[string]*wechatLoginSession),
			contextTokens: make(map[string]string),
		}, nil
	})
}

func (w *WechatPersonalChannel) Name() string { return "wechat_personal" }

func (w *WechatPersonalChannel) Configure(cfg config.WechatPersonalConfig, workspace string) {
	w.workspace = workspace
	w.baseURL = cfg.BaseURL
	w.cdnBaseURL = cfg.CDNBaseURL
	w.botType = cfg.BotType
	w.pollTimeoutMS = cfg.PollTimeoutMS
	w.loginTimeoutS = cfg.LoginTimeoutS
	w.sessionPauseMinutes = cfg.SessionPauseMinutes
	w.requestTimeoutMS = cfg.RequestTimeoutMS
	w.configTimeoutMS = cfg.ConfigTimeoutMS
	w.AllowFrom = cfg.AllowFrom
	if cfg.StateDir != "" {
		w.stateDir = config.ExpandHome(cfg.StateDir)
	} else {
		w.stateDir = filepath.Join(workspace, "channels", "wechat_personal")
	}
	for accountID, acfg := range cfg.Accounts {
		account := w.ensureAccount(accountID)
		account.baseURL = firstNonEmpty(acfg.BaseURL, cfg.BaseURL, w.baseURL)
		account.cdnBaseURL = firstNonEmpty(acfg.CDNBaseURL, cfg.CDNBaseURL, w.cdnBaseURL)
		account.botType = firstNonEmpty(acfg.BotType, cfg.BotType, w.botType)
		if len(acfg.AllowFrom) > 0 {
			account.allowFrom = append([]string(nil), acfg.AllowFrom...)
		}
		if acfg.ILinkUserID != "" {
			account.ilinkUserID = acfg.ILinkUserID
		}
		account.pollTimeoutMS = cfg.PollTimeoutMS
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (w *WechatPersonalChannel) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		<-ctx.Done()
		return nil
	}
	w.started = true
	w.rootCtx, w.rootCancel = context.WithCancel(ctx)
	w.Running = true
	w.mu.Unlock()

	if err := w.ensureStateDirs(); err != nil {
		return err
	}
	if err := w.loadAccountsFromState(); err != nil {
		return err
	}

	for _, accountID := range w.listAccountIDs() {
		account := w.lookupAccount(accountID)
		if account == nil {
			continue
		}
		if strings.TrimSpace(account.token) == "" {
			continue
		}
		w.startAccountMonitor(account)
	}

	<-w.rootCtx.Done()
	w.stopAllAccounts()
	return nil
}

func (w *WechatPersonalChannel) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Running = false
	if w.rootCancel != nil {
		w.rootCancel()
	}
	w.stopAllAccounts()
	return nil
}

func (w *WechatPersonalChannel) stopAllAccounts() {
	w.mu.RLock()
	accounts := make([]*wechatAccountRuntime, 0, len(w.accounts))
	for _, account := range w.accounts {
		accounts = append(accounts, account)
	}
	w.mu.RUnlock()

	for _, account := range accounts {
		account.mu.Lock()
		cancel := account.cancel
		account.cancel = nil
		account.running = false
		account.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

func (w *WechatPersonalChannel) Send(msg *bus.OutboundMessage) error {
	account, err := w.resolveAccountForOutbound(msg.AccountID)
	if err != nil {
		return err
	}
	account.mu.RLock()
	contextToken := w.lookupContextToken(account.id, msg.ChatID)
	account.mu.RUnlock()
	if contextToken == "" {
		return fmt.Errorf("wechat context_token missing for account=%s peer=%s", account.id, msg.ChatID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if isProgressMessage(msg) {
		return w.sendTyping(ctx, account, wechatTypingStatusTyping, contextToken)
	}
	_ = w.sendTyping(ctx, account, wechatTypingStatusCancel, contextToken)

	if strings.TrimSpace(msg.Content) != "" {
		if err := w.sendOutboundItem(ctx, account, msg.ChatID, contextToken, buildTextItem(msg.Content)); err != nil {
			return err
		}
	}
	for _, mediaPath := range msg.Media {
		uploaded, mediaType, err := w.uploadOutboundMedia(ctx, account, mediaPath, msg.ChatID)
		if err != nil {
			return err
		}
		if err := w.sendOutboundItem(ctx, account, msg.ChatID, contextToken, buildMediaItem(mediaType, uploaded)); err != nil {
			return err
		}
	}
	return nil
}

func isProgressMessage(msg *bus.OutboundMessage) bool {
	if msg == nil || msg.Metadata == nil {
		return false
	}
	progress, _ := msg.Metadata["_progress"].(bool)
	return progress
}

func (w *WechatPersonalChannel) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/channels/wechat_personal/login/start", w.handleLoginStart)
	mux.HandleFunc("/channels/wechat_personal/login/wait", w.handleLoginWait)
	mux.HandleFunc("/channels/wechat_personal/status", w.handleStatus)
}

func (w *WechatPersonalChannel) Status() map[string]any {
	status := make([]wechatAccountStatus, 0)
	for _, accountID := range w.listAccountIDs() {
		account := w.lookupAccount(accountID)
		if account == nil {
			continue
		}
		account.mu.RLock()
		status = append(status, wechatAccountStatus{
			AccountID:     account.id,
			Active:        account.running,
			LoggedIn:      account.token != "",
			BaseURL:       account.baseURL,
			CDNBaseURL:    account.cdnBaseURL,
			ILinkUserID:   account.ilinkUserID,
			PausedUntil:   account.pausedUntil,
			LastError:     account.lastError,
			LastSeenAt:    account.lastSeenAt,
			LastMessageAt: account.lastMessageAt,
		})
		account.mu.RUnlock()
	}
	return map[string]any{"accounts": status}
}

func (w *WechatPersonalChannel) handleStatus(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(rw, http.StatusOK, w.Status())
}

type wechatLoginStartRequest struct {
	AccountID string `json:"account_id"`
	BaseURL   string `json:"base_url"`
	BotType   string `json:"bot_type"`
	Force     bool   `json:"force"`
}

type wechatLoginWaitRequest struct {
	SessionKey string `json:"session_key"`
	TimeoutMS  int    `json:"timeout_ms"`
}

func (w *WechatPersonalChannel) handleLoginStart(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload wechatLoginStartRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil && err.Error() != "EOF" {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	accountID := strings.TrimSpace(payload.AccountID)
	if accountID == "" {
		accountID = "wx_" + uuid.NewString()[:8]
	}
	baseURL := firstNonEmpty(payload.BaseURL, w.baseURL)
	botType := firstNonEmpty(payload.BotType, w.botType, "3")

	w.mu.Lock()
	existing := w.logins[accountID]
	if existing != nil && !payload.Force && time.Since(existing.StartedAt) < 5*time.Minute {
		w.mu.Unlock()
		writeJSON(rw, http.StatusOK, map[string]any{
			"session_key": existing.SessionKey,
			"account_id":  existing.AccountID,
			"qrcode_url":  existing.QRCodeURL,
			"message":     "二维码已就绪，请扫描确认。",
		})
		return
	}
	w.mu.Unlock()

	ctx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	defer cancel()
	qr, err := w.fetchQRCode(ctx, baseURL, botType)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	session := &wechatLoginSession{
		SessionKey: accountID,
		AccountID:  accountID,
		QRCode:     qr.QRCode,
		QRCodeURL:  qr.QRCodeImgURL,
		StartedAt:  time.Now(),
	}
	w.mu.Lock()
	w.logins[session.SessionKey] = session
	account := w.ensureAccount(accountID)
	account.baseURL = firstNonEmpty(account.baseURL, baseURL)
	account.botType = firstNonEmpty(account.botType, botType)
	account.cdnBaseURL = firstNonEmpty(account.cdnBaseURL, w.cdnBaseURL)
	w.mu.Unlock()

	writeJSON(rw, http.StatusOK, map[string]any{
		"session_key": session.SessionKey,
		"account_id":  session.AccountID,
		"qrcode_url":  session.QRCodeURL,
		"message":     "请使用微信扫描二维码并在手机上确认授权。",
	})
}

func (w *WechatPersonalChannel) handleLoginWait(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload wechatLoginWaitRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	w.mu.RLock()
	session := w.logins[payload.SessionKey]
	w.mu.RUnlock()
	if session == nil {
		http.Error(rw, "login session not found", http.StatusNotFound)
		return
	}
	account := w.lookupAccount(session.AccountID)
	if account == nil {
		http.Error(rw, "account runtime not found", http.StatusInternalServerError)
		return
	}

	timeoutMS := payload.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = w.loginTimeoutS * 1000
	}
	ctx, cancel := context.WithTimeout(req.Context(), time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			writeJSON(rw, http.StatusOK, map[string]any{
				"connected": false,
				"message":   "等待扫码确认超时。",
			})
			return
		default:
		}

		status, err := w.pollQRCodeStatus(ctx, account.baseURL, session.QRCode)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadGateway)
			return
		}
		switch status.Status {
		case "confirmed":
			account.mu.Lock()
			account.token = status.BotToken
			account.ilinkBotID = status.ILinkBotID
			account.ilinkUserID = status.ILinkUserID
			account.baseURL = firstNonEmpty(status.BaseURL, account.baseURL, w.baseURL)
			account.cdnBaseURL = firstNonEmpty(account.cdnBaseURL, w.cdnBaseURL)
			account.botType = firstNonEmpty(account.botType, w.botType, "3")
			account.lastError = ""
			account.pausedUntil = time.Time{}
			account.mu.Unlock()
			if err := w.saveAccountState(account); err != nil {
				http.Error(rw, err.Error(), http.StatusInternalServerError)
				return
			}
			w.mu.Lock()
			delete(w.logins, payload.SessionKey)
			w.mu.Unlock()
			w.startAccountMonitor(account)
			writeJSON(rw, http.StatusOK, map[string]any{
				"connected":  true,
				"account_id": account.id,
				"user_id":    account.ilinkUserID,
				"base_url":   account.baseURL,
				"message":    "微信账号已连接。",
			})
			return
		case "expired":
			w.mu.Lock()
			delete(w.logins, payload.SessionKey)
			w.mu.Unlock()
			writeJSON(rw, http.StatusOK, map[string]any{
				"connected": false,
				"message":   "二维码已过期，请重新生成。",
			})
			return
		default:
			time.Sleep(2 * time.Second)
		}
	}
}

func writeJSON(rw http.ResponseWriter, status int, payload any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(payload)
}

func (w *WechatPersonalChannel) ensureAccount(accountID string) *wechatAccountRuntime {
	if accountID == "" {
		accountID = "default"
	}
	account, ok := w.accounts[accountID]
	if ok {
		return account
	}
	account = &wechatAccountRuntime{
		id:            accountID,
		baseURL:       w.baseURL,
		cdnBaseURL:    w.cdnBaseURL,
		botType:       firstNonEmpty(w.botType, "3"),
		allowFrom:     append([]string(nil), w.AllowFrom...),
		pollTimeoutMS: w.pollTimeoutMS,
	}
	w.accounts[accountID] = account
	return account
}

func (w *WechatPersonalChannel) lookupAccount(accountID string) *wechatAccountRuntime {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.accounts[accountID]
}

func (w *WechatPersonalChannel) loadAccountsFromState() error {
	states, err := w.loadPersistedStates()
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for accountID, state := range states {
		account := w.ensureAccount(accountID)
		account.token = state.Token
		account.ilinkUserID = state.ILinkUserID
		account.ilinkBotID = state.ILinkBotID
		account.baseURL = firstNonEmpty(account.baseURL, state.BaseURL, w.baseURL)
		account.cdnBaseURL = firstNonEmpty(account.cdnBaseURL, state.CDNBaseURL, w.cdnBaseURL)
		account.botType = firstNonEmpty(account.botType, state.BotType, w.botType, "3")
		account.getUpdatesBuf = state.GetUpdatesBuf
		account.pausedUntil = state.PausedUntil
		account.lastError = state.LastError
		account.lastSeenAt = state.LastSeenAt
		account.lastMessageAt = state.LastMessageAt
		account.typingTicket = state.TypingTicket
	}
	return nil
}

func (w *WechatPersonalChannel) resolveAccountForOutbound(accountID string) (*wechatAccountRuntime, error) {
	if accountID != "" {
		account := w.lookupAccount(accountID)
		if account == nil {
			return nil, fmt.Errorf("wechat account not found: %s", accountID)
		}
		return account, nil
	}
	ids := w.listAccountIDs()
	if len(ids) == 1 {
		return w.lookupAccount(ids[0]), nil
	}
	return nil, fmt.Errorf("wechat account_id is required when multiple accounts are configured")
}

func (w *WechatPersonalChannel) startAccountMonitor(account *wechatAccountRuntime) {
	if account == nil || w.rootCtx == nil {
		return
	}
	account.mu.Lock()
	if account.running || account.token == "" {
		account.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(w.rootCtx)
	account.cancel = cancel
	account.running = true
	account.mu.Unlock()

	go func() {
		defer func() {
			account.mu.Lock()
			account.running = false
			account.cancel = nil
			account.mu.Unlock()
		}()
		w.monitorAccount(ctx, account)
	}()
}

func (w *WechatPersonalChannel) monitorAccount(ctx context.Context, account *wechatAccountRuntime) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		account.mu.RLock()
		if !account.pausedUntil.IsZero() && time.Now().Before(account.pausedUntil) {
			sleepFor := time.Until(account.pausedUntil)
			account.mu.RUnlock()
			timer := time.NewTimer(sleepFor)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}
		cursor := account.getUpdatesBuf
		account.mu.RUnlock()

		resp, err := w.getUpdates(ctx, account, cursor)
		if err != nil {
			account.mu.Lock()
			account.lastError = err.Error()
			account.mu.Unlock()
			_ = w.saveAccountState(account)
			time.Sleep(5 * time.Second)
			continue
		}
		if resp.ErrCode == -14 {
			account.mu.Lock()
			account.lastError = firstNonEmpty(resp.ErrMsg, "session expired")
			account.pausedUntil = time.Now().Add(time.Duration(w.sessionPauseMinutes) * time.Minute)
			account.mu.Unlock()
			_ = w.saveAccountState(account)
			continue
		}

		account.mu.Lock()
		if resp.GetUpdatesBuf != "" {
			account.getUpdatesBuf = resp.GetUpdatesBuf
		}
		account.lastSeenAt = time.Now()
		account.lastError = ""
		account.mu.Unlock()
		_ = w.saveAccountState(account)

		for _, msg := range resp.Msgs {
			if err := w.processInboundMessage(account, msg); err != nil {
				w.Logger.Warn("处理微信消息失败",
					zap.String("account_id", account.id),
					zap.Error(err),
				)
			}
		}
	}
}

func (w *WechatPersonalChannel) processInboundMessage(account *wechatAccountRuntime, msg *wechatMessage) error {
	if msg == nil || msg.FromUserID == "" {
		return nil
	}
	if msg.MessageType == wechatMessageTypeBot {
		return nil
	}
	contextToken := strings.TrimSpace(msg.ContextToken)
	if contextToken != "" {
		w.setContextToken(account.id, msg.FromUserID, contextToken)
	}

	content, media, err := w.extractInboundContent(account, msg)
	if err != nil {
		return err
	}
	if content == "" && len(media) == 0 {
		return nil
	}
	if !w.isAllowedSender(account, msg.FromUserID) {
		return nil
	}

	account.mu.Lock()
	account.lastMessageAt = time.Now()
	account.mu.Unlock()
	_ = w.saveAccountState(account)

	inbound := bus.NewInboundMessage("wechat_personal", msg.FromUserID, msg.FromUserID, content)
	inbound.AccountID = account.id
	inbound.Media = media
	inbound.Metadata["message_id"] = fmt.Sprintf("%d", msg.MessageID)
	inbound.Metadata["context_token"] = contextToken
	inbound.Metadata["account_id"] = account.id
	inbound.Metadata["chat_type"] = "direct"
	inbound.Metadata["session_id"] = msg.SessionID
	if msg.CreateTimeMS > 0 {
		inbound.Timestamp = time.UnixMilli(msg.CreateTimeMS)
	}
	w.Bus.PublishInbound(inbound)
	return nil
}

func (w *WechatPersonalChannel) extractInboundContent(account *wechatAccountRuntime, msg *wechatMessage) (string, []string, error) {
	var content string
	var media []string
	for _, item := range msg.ItemList {
		if item == nil {
			continue
		}
		switch item.Type {
		case wechatMessageItemText:
			if item.TextItem != nil && strings.TrimSpace(item.TextItem.Text) != "" {
				if content == "" {
					content = strings.TrimSpace(item.TextItem.Text)
				} else {
					content += "\n" + strings.TrimSpace(item.TextItem.Text)
				}
			}
		case wechatMessageItemVoice:
			if item.VoiceItem != nil && strings.TrimSpace(item.VoiceItem.Text) != "" && content == "" {
				content = strings.TrimSpace(item.VoiceItem.Text)
			}
			if item.VoiceItem != nil {
				path, err := w.downloadInboundMedia(account.cdnBaseURL, item.VoiceItem.Media, "", "voice.wav")
				if err != nil {
					return "", nil, err
				}
				if path != "" {
					media = append(media, path)
				}
			}
		case wechatMessageItemImage:
			if item.ImageItem != nil {
				path, err := w.downloadInboundMedia(account.cdnBaseURL, item.ImageItem.Media, item.ImageItem.AESKeyHex, "image.jpg")
				if err != nil {
					return "", nil, err
				}
				if path != "" {
					media = append(media, path)
				}
			}
		case wechatMessageItemFile:
			if item.FileItem != nil {
				path, err := w.downloadInboundMedia(account.cdnBaseURL, item.FileItem.Media, "", item.FileItem.FileName)
				if err != nil {
					return "", nil, err
				}
				if path != "" {
					media = append(media, path)
				}
			}
		case wechatMessageItemVideo:
			if item.VideoItem != nil {
				path, err := w.downloadInboundMedia(account.cdnBaseURL, item.VideoItem.Media, "", "video.mp4")
				if err != nil {
					return "", nil, err
				}
				if path != "" {
					media = append(media, path)
				}
			}
		}
	}
	return content, media, nil
}

func (w *WechatPersonalChannel) isAllowedSender(account *wechatAccountRuntime, senderID string) bool {
	account.mu.RLock()
	allowFrom := append([]string(nil), account.allowFrom...)
	account.mu.RUnlock()
	if len(allowFrom) == 0 {
		allowFrom = w.AllowFrom
	}
	if len(allowFrom) == 0 {
		return true
	}
	for _, allowed := range allowFrom {
		if allowed == senderID {
			return true
		}
	}
	return false
}

func (w *WechatPersonalChannel) setContextToken(accountID, userID, token string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.BaseChannel.Logger != nil {
		w.BaseChannel.Logger.Debug("缓存微信 context token",
			zap.String("account_id", accountID),
			zap.String("user_id", userID),
		)
	}
	key := accountID + ":" + userID
	if w.logins == nil {
		w.logins = make(map[string]*wechatLoginSession)
	}
	if w.accounts == nil {
		w.accounts = make(map[string]*wechatAccountRuntime)
	}
	if w.contextTokens == nil {
		w.contextTokens = make(map[string]string)
	}
	w.contextTokens[key] = token
}

func (w *WechatPersonalChannel) lookupContextToken(accountID, userID string) string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.contextTokens == nil {
		return ""
	}
	return w.contextTokens[accountID+":"+userID]
}
