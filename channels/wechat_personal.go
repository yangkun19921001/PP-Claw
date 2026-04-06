package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/config"
	"github.com/yangkun19921001/PP-Claw/utils"
	"go.uber.org/zap"
)

const (
	wechatMaxMessageLen             = 4000
	wechatProcessedMessageCacheSize = 1000
)

var (
	wechatTypingKeepaliveInterval    = 5 * time.Second
	wechatTypingTicketInitialBackoff = 2 * time.Second
	wechatTypingTicketMaxBackoff     = time.Hour
	wechatTypingTicketTTL            = 24 * time.Hour
)

type wechatTypingLoop struct {
	cancel context.CancelFunc
}

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
	pausedUntil   time.Time
	lastError     string
	lastSeenAt    time.Time
	lastMessageAt time.Time
	running       bool
	cancel        context.CancelFunc
	pollTimeoutMS int
	contextTokens map[string]string
	typingTickets map[string]*wechatTypingTicketState
	typingCancels map[string]*wechatTypingLoop
	processedIDs  *utils.LRUCache
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

	httpClient *http.Client
	accounts   map[string]*wechatAccountRuntime
	logins     map[string]*wechatLoginSession
	started    bool
	rootCtx    context.Context
	rootCancel context.CancelFunc
}

func init() {
	RegisterFactory("wechat_personal", func(msgBus *bus.MessageBus, logger *zap.Logger) (Channel, error) {
		return &WechatPersonalChannel{
			BaseChannel: BaseChannel{
				ChannelName: "wechat_personal",
				Bus:         msgBus,
				Logger:      logger,
			},
			httpClient: &http.Client{Timeout: 60 * time.Second},
			accounts:   make(map[string]*wechatAccountRuntime),
			logins:     make(map[string]*wechatLoginSession),
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
		typingCancels := make([]context.CancelFunc, 0, len(account.typingCancels))
		for _, typingLoop := range account.typingCancels {
			if typingLoop != nil && typingLoop.cancel != nil {
				typingCancels = append(typingCancels, typingLoop.cancel)
			}
		}
		account.typingCancels = make(map[string]*wechatTypingLoop)
		account.cancel = nil
		account.running = false
		account.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		for _, typingCancel := range typingCancels {
			if typingCancel != nil {
				typingCancel()
			}
		}
	}
}

func (w *WechatPersonalChannel) Send(msg *bus.OutboundMessage) error {
	account, err := w.resolveAccountForOutbound(msg.AccountID)
	if err != nil {
		return err
	}
	contextToken := w.lookupContextToken(account, msg.ChatID)
	w.Logger.Info("微信出站开始",
		zap.String("account_id", account.id),
		zap.String("chat_id", msg.ChatID),
		zap.String("reply_to", msg.ReplyTo),
		zap.Int("content_len", len(msg.Content)),
		zap.Int("media_count", len(msg.Media)),
		zap.Bool("has_context_token", contextToken != ""),
	)
	if contextToken == "" {
		return fmt.Errorf("wechat context_token missing for account=%s peer=%s", account.id, msg.ChatID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if isProgressMessage(msg) {
		return w.ensureTypingLoop(account, msg.ChatID, contextToken)
	}
	typingStarted := false
	if err := w.ensureTypingLoop(account, msg.ChatID, contextToken); err == nil {
		typingStarted = true
	}
	defer func() {
		if typingStarted {
			_ = w.stopTypingLoop(account, msg.ChatID, true)
		}
	}()

	for _, mediaPath := range msg.Media {
		stat, statErr := os.Stat(mediaPath)
		fields := []zap.Field{
			zap.String("account_id", account.id),
			zap.String("chat_id", msg.ChatID),
			zap.String("media_path", mediaPath),
			zap.String("media_ext", strings.ToLower(filepath.Ext(mediaPath))),
		}
		if statErr == nil {
			fields = append(fields, zap.Int64("media_size", stat.Size()))
		}
		w.Logger.Info("微信出站媒体准备上传", fields...)

		uploaded, mediaType, err := w.uploadOutboundMedia(ctx, account, mediaPath, msg.ChatID)
		if err != nil {
			w.Logger.Error("微信出站媒体上传失败",
				zap.String("account_id", account.id),
				zap.String("chat_id", msg.ChatID),
				zap.String("media_path", mediaPath),
				zap.Error(err),
			)
			return err
		}
		item := buildMediaItem(mediaType, uploaded)
		w.Logger.Info("微信出站媒体上传完成",
			zap.String("account_id", account.id),
			zap.String("chat_id", msg.ChatID),
			zap.String("media_path", mediaPath),
			zap.String("media_type", wechatMediaTypeName(mediaType)),
			zap.String("item_type", wechatMessageItemTypeName(item.Type)),
			zap.Int("raw_size", uploaded.FileSize),
			zap.Int("cipher_size", uploaded.FileSizeCiphertext),
			zap.Bool("has_download_param", strings.TrimSpace(uploaded.DownloadParam) != ""),
		)
		if err := w.sendOutboundItem(ctx, account, msg.ChatID, contextToken, item); err != nil {
			w.Logger.Error("微信出站媒体消息发送失败",
				zap.String("account_id", account.id),
				zap.String("chat_id", msg.ChatID),
				zap.String("media_path", mediaPath),
				zap.String("media_type", wechatMediaTypeName(mediaType)),
				zap.String("item_type", wechatMessageItemTypeName(item.Type)),
				zap.Error(err),
			)
			return err
		}
		w.Logger.Info("微信出站媒体消息发送完成",
			zap.String("account_id", account.id),
			zap.String("chat_id", msg.ChatID),
			zap.String("media_path", mediaPath),
			zap.String("media_type", wechatMediaTypeName(mediaType)),
			zap.String("item_type", wechatMessageItemTypeName(item.Type)),
		)
	}
	chunks := splitWechatMessage(strings.TrimSpace(msg.Content), wechatMaxMessageLen)
	for idx, chunk := range chunks {
		w.Logger.Info("微信出站文本消息发送",
			zap.String("account_id", account.id),
			zap.String("chat_id", msg.ChatID),
			zap.Int("chunk_index", idx+1),
			zap.Int("chunk_total", len(chunks)),
			zap.Int("chunk_len", len(chunk)),
		)
		if err := w.sendOutboundItem(ctx, account, msg.ChatID, contextToken, buildTextItem(chunk)); err != nil {
			w.Logger.Error("微信出站文本消息发送失败",
				zap.String("account_id", account.id),
				zap.String("chat_id", msg.ChatID),
				zap.Int("chunk_index", idx+1),
				zap.Int("chunk_total", len(chunks)),
				zap.Error(err),
			)
			return err
		}
	}
	w.Logger.Info("微信出站完成",
		zap.String("account_id", account.id),
		zap.String("chat_id", msg.ChatID),
		zap.Int("media_count", len(msg.Media)),
		zap.Int("text_chunks", len(chunks)),
	)
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
		if account.contextTokens == nil {
			account.contextTokens = make(map[string]string)
		}
		if account.typingTickets == nil {
			account.typingTickets = make(map[string]*wechatTypingTicketState)
		}
		if account.typingCancels == nil {
			account.typingCancels = make(map[string]*wechatTypingLoop)
		}
		if account.processedIDs == nil {
			account.processedIDs = utils.NewLRUCache(wechatProcessedMessageCacheSize)
		}
		return account
	}
	account = &wechatAccountRuntime{
		id:            accountID,
		baseURL:       w.baseURL,
		cdnBaseURL:    w.cdnBaseURL,
		botType:       firstNonEmpty(w.botType, "3"),
		allowFrom:     append([]string(nil), w.AllowFrom...),
		pollTimeoutMS: w.pollTimeoutMS,
		contextTokens: make(map[string]string),
		typingTickets: make(map[string]*wechatTypingTicketState),
		typingCancels: make(map[string]*wechatTypingLoop),
		processedIDs:  utils.NewLRUCache(wechatProcessedMessageCacheSize),
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
		account.contextTokens = cloneStringMap(state.ContextTokens)
		account.typingTickets = cloneTypingTicketMap(state.TypingTickets)
		if account.typingCancels == nil {
			account.typingCancels = make(map[string]*wechatTypingLoop)
		}
		if account.processedIDs == nil {
			account.processedIDs = utils.NewLRUCache(wechatProcessedMessageCacheSize)
		}
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
		if resp.LongPollingTimeout > 0 {
			account.pollTimeoutMS = resp.LongPollingTimeout
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
	msgID := buildWechatMessageID(msg)
	account.mu.RLock()
	if account.processedIDs != nil && msgID != "" && account.processedIDs.Contains(msgID) {
		account.mu.RUnlock()
		return nil
	}
	account.mu.RUnlock()
	if msgID != "" {
		account.mu.Lock()
		if account.processedIDs == nil {
			account.processedIDs = utils.NewLRUCache(wechatProcessedMessageCacheSize)
		}
		account.processedIDs.Add(msgID)
		account.mu.Unlock()
	}

	contextToken := strings.TrimSpace(msg.ContextToken)
	if contextToken != "" {
		w.setContextToken(account, msg.FromUserID, contextToken)
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
	if contextToken != "" {
		go func() {
			if err := w.ensureTypingLoop(account, msg.FromUserID, contextToken); err != nil && w.Logger != nil {
				w.Logger.Debug("启动微信 typing 失败",
					zap.String("account_id", account.id),
					zap.String("user_id", msg.FromUserID),
					zap.Error(err),
				)
			}
		}()
	}

	inbound := bus.NewInboundMessage("wechat_personal", msg.FromUserID, msg.FromUserID, content)
	inbound.AccountID = account.id
	inbound.Media = media
	inbound.Metadata["message_id"] = msgID
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
	var parts []string
	var media []string
	hasTopLevelDownloadableMedia := false
	for _, item := range msg.ItemList {
		if item == nil {
			continue
		}
		switch item.Type {
		case wechatMessageItemText:
			text := ""
			if item.TextItem != nil {
				text = strings.TrimSpace(item.TextItem.Text)
			}
			if text == "" {
				continue
			}
			if item.RefMsg != nil {
				refItem := item.RefMsg.MessageItem
				if refItem != nil && (refItem.Type == wechatMessageItemImage || refItem.Type == wechatMessageItemVoice || refItem.Type == wechatMessageItemFile || refItem.Type == wechatMessageItemVideo) {
					parts = append(parts, text)
					continue
				}
				refParts := make([]string, 0, 2)
				if title := strings.TrimSpace(item.RefMsg.Title); title != "" {
					refParts = append(refParts, title)
				}
				if refItem != nil && refItem.TextItem != nil {
					if refText := strings.TrimSpace(refItem.TextItem.Text); refText != "" {
						refParts = append(refParts, refText)
					}
				}
				if len(refParts) > 0 {
					parts = append(parts, fmt.Sprintf("[引用: %s]\n%s", strings.Join(refParts, " | "), text))
					continue
				}
			}
			parts = append(parts, text)
		case wechatMessageItemVoice:
			if item.VoiceItem != nil {
				if text := strings.TrimSpace(item.VoiceItem.Text); text != "" {
					parts = append(parts, "[voice] "+text)
				}
				if hasDownloadableMedia(item.VoiceItem.Media) {
					hasTopLevelDownloadableMedia = true
				}
				path, err := w.downloadInboundMedia(account.cdnBaseURL, item.VoiceItem.Media, "", "voice.wav", "voice")
				if err != nil {
					if w.Logger != nil {
						w.Logger.Warn("下载微信语音失败", zap.String("account_id", account.id), zap.Error(err))
					}
					path = ""
				}
				if path != "" {
					if strings.TrimSpace(item.VoiceItem.Text) == "" {
						parts = append(parts, fmt.Sprintf("[voice]\n[Audio: source: %s]", path))
					}
					media = append(media, path)
				} else if strings.TrimSpace(item.VoiceItem.Text) == "" {
					parts = append(parts, "[voice]")
				}
			}
		case wechatMessageItemImage:
			if item.ImageItem != nil {
				if hasDownloadableMedia(item.ImageItem.Media) {
					hasTopLevelDownloadableMedia = true
				}
				path, err := w.downloadInboundMedia(account.cdnBaseURL, item.ImageItem.Media, item.ImageItem.AESKeyHex, "image.jpg", "image")
				if err != nil {
					if w.Logger != nil {
						w.Logger.Warn("下载微信图片失败", zap.String("account_id", account.id), zap.Error(err))
					}
					path = ""
				}
				if path != "" {
					parts = append(parts, fmt.Sprintf("[image]\n[Image: source: %s]", path))
					media = append(media, path)
				} else {
					parts = append(parts, "[image]")
				}
			}
		case wechatMessageItemFile:
			if item.FileItem != nil {
				if hasDownloadableMedia(item.FileItem.Media) {
					hasTopLevelDownloadableMedia = true
				}
				name := firstNonEmpty(item.FileItem.FileName, "unknown")
				path, err := w.downloadInboundMedia(account.cdnBaseURL, item.FileItem.Media, "", name, "file")
				if err != nil {
					if w.Logger != nil {
						w.Logger.Warn("下载微信文件失败", zap.String("account_id", account.id), zap.Error(err))
					}
					path = ""
				}
				if path != "" {
					parts = append(parts, fmt.Sprintf("[file: %s]\n[File: source: %s]", name, path))
					media = append(media, path)
				} else {
					parts = append(parts, fmt.Sprintf("[file: %s]", name))
				}
			}
		case wechatMessageItemVideo:
			if item.VideoItem != nil {
				if hasDownloadableMedia(item.VideoItem.Media) {
					hasTopLevelDownloadableMedia = true
				}
				path, err := w.downloadInboundMedia(account.cdnBaseURL, item.VideoItem.Media, "", "video.mp4", "video")
				if err != nil {
					if w.Logger != nil {
						w.Logger.Warn("下载微信视频失败", zap.String("account_id", account.id), zap.Error(err))
					}
					path = ""
				}
				if path != "" {
					parts = append(parts, fmt.Sprintf("[video]\n[Video: source: %s]", path))
					media = append(media, path)
				} else {
					parts = append(parts, "[video]")
				}
			}
		}
	}

	if len(media) == 0 && !hasTopLevelDownloadableMedia {
		for _, item := range msg.ItemList {
			if item == nil || item.Type != wechatMessageItemText || item.RefMsg == nil || item.RefMsg.MessageItem == nil {
				continue
			}
			refItem := item.RefMsg.MessageItem
			var (
				path string
				err  error
			)
			switch refItem.Type {
			case wechatMessageItemImage:
				if refItem.ImageItem != nil {
					path, err = w.downloadInboundMedia(account.cdnBaseURL, refItem.ImageItem.Media, refItem.ImageItem.AESKeyHex, "image.jpg", "image")
					if path != "" {
						parts = append(parts, fmt.Sprintf("[image]\n[Image: source: %s]", path))
					}
				}
			case wechatMessageItemVoice:
				if refItem.VoiceItem != nil {
					path, err = w.downloadInboundMedia(account.cdnBaseURL, refItem.VoiceItem.Media, "", "voice.wav", "voice")
					if path != "" {
						if text := strings.TrimSpace(refItem.VoiceItem.Text); text != "" {
							parts = append(parts, "[voice] "+text)
						} else {
							parts = append(parts, fmt.Sprintf("[voice]\n[Audio: source: %s]", path))
						}
					}
				}
			case wechatMessageItemFile:
				if refItem.FileItem != nil {
					name := firstNonEmpty(refItem.FileItem.FileName, "unknown")
					path, err = w.downloadInboundMedia(account.cdnBaseURL, refItem.FileItem.Media, "", name, "file")
					if path != "" {
						parts = append(parts, fmt.Sprintf("[file: %s]\n[File: source: %s]", name, path))
					}
				}
			case wechatMessageItemVideo:
				if refItem.VideoItem != nil {
					path, err = w.downloadInboundMedia(account.cdnBaseURL, refItem.VideoItem.Media, "", "video.mp4", "video")
					if path != "" {
						parts = append(parts, fmt.Sprintf("[video]\n[Video: source: %s]", path))
					}
				}
			}
			if err != nil {
				if w.Logger != nil {
					w.Logger.Warn("下载微信引用媒体失败", zap.String("account_id", account.id), zap.Error(err))
				}
				path = ""
			}
			if path != "" {
				media = append(media, path)
				break
			}
		}
	}

	return strings.Join(parts, "\n"), media, nil
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

func (w *WechatPersonalChannel) setContextToken(account *wechatAccountRuntime, userID, token string) {
	if account == nil || userID == "" || token == "" {
		return
	}
	account.mu.Lock()
	if account.contextTokens == nil {
		account.contextTokens = make(map[string]string)
	}
	account.contextTokens[userID] = token
	account.mu.Unlock()
	if w.BaseChannel.Logger != nil {
		w.BaseChannel.Logger.Debug("缓存微信 context token",
			zap.String("account_id", account.id),
			zap.String("user_id", userID),
		)
	}
}

func (w *WechatPersonalChannel) lookupContextToken(account *wechatAccountRuntime, userID string) string {
	if account == nil || userID == "" {
		return ""
	}
	account.mu.RLock()
	defer account.mu.RUnlock()
	if account.contextTokens == nil {
		return ""
	}
	return account.contextTokens[userID]
}

func buildWechatMessageID(msg *wechatMessage) string {
	if msg == nil {
		return ""
	}
	if msg.MessageID != 0 {
		return fmt.Sprintf("%d", msg.MessageID)
	}
	if msg.Seq != 0 {
		return fmt.Sprintf("%d", msg.Seq)
	}
	if msg.FromUserID != "" || msg.CreateTimeMS != 0 {
		return fmt.Sprintf("%s_%d", msg.FromUserID, msg.CreateTimeMS)
	}
	return ""
}

func hasDownloadableMedia(media *wechatCDNMedia) bool {
	if media == nil {
		return false
	}
	return strings.TrimSpace(media.EncryptQueryParam) != "" || strings.TrimSpace(media.FullURL) != ""
}

func splitWechatMessage(content string, maxLen int) []string {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if maxLen <= 0 {
		maxLen = wechatMaxMessageLen
	}
	runes := []rune(content)
	if len(runes) <= maxLen {
		return []string{content}
	}
	chunks := make([]string, 0, (len(runes)/maxLen)+1)
	for len(runes) > 0 {
		end := maxLen
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[:end]))
		runes = runes[end:]
	}
	return chunks
}
