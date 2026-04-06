package channels

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	wechatAPIPathGetUpdates   = "ilink/bot/getupdates"
	wechatAPIPathSendMessage  = "ilink/bot/sendmessage"
	wechatAPIPathGetUploadURL = "ilink/bot/getuploadurl"
	wechatAPIPathGetConfig    = "ilink/bot/getconfig"
	wechatAPIPathSendTyping   = "ilink/bot/sendtyping"
	wechatAPIChannelVersion   = "pp-claw-wechat-personal/0.1"
	wechatQRStatusLongPollMS  = 35000
	wechatDefaultAPITimeoutMS = 15000
	wechatDefaultCfgTimeoutMS = 10000
)

func buildWechatBaseURL(baseURL, endpoint string) (string, error) {
	if baseURL == "" {
		return "", fmt.Errorf("base_url is required")
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	base.Path = path.Join(base.Path, endpoint)
	return base.String(), nil
}

func randomWechatUIN() (string, error) {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	num := binary.BigEndian.Uint32(raw[:])
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", num))), nil
}

func (w *WechatPersonalChannel) apiPost(ctx context.Context, baseURL, endpoint, token string, body any, timeoutMS int, out any) error {
	payload := body
	if payload == nil {
		payload = map[string]any{}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	targetURL, err := buildWechatBaseURL(baseURL, endpoint)
	if err != nil {
		return err
	}
	uin, err := randomWechatUIN()
	if err != nil {
		return err
	}

	timeout := timeoutMS
	if timeout <= 0 {
		timeout = wechatDefaultAPITimeoutMS
	}
	reqCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		reqCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, targetURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(data)))
	req.Header.Set("X-WECHAT-UIN", uin)
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("wechat API %s %d: %s", endpoint, resp.StatusCode, string(raw))
	}
	if out == nil {
		return nil
	}
	if len(raw) == 0 {
		return fmt.Errorf("wechat API %s returned empty body", endpoint)
	}
	return json.Unmarshal(raw, out)
}

func (w *WechatPersonalChannel) fetchQRCode(ctx context.Context, baseURL, botType string) (*wechatQRCodeResponse, error) {
	if botType == "" {
		botType = "3"
	}
	targetURL, err := buildWechatBaseURL(baseURL, "ilink/bot/get_bot_qrcode")
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("bot_type", botType)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("wechat get_bot_qrcode %d: %s", resp.StatusCode, string(raw))
	}
	var result wechatQRCodeResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (w *WechatPersonalChannel) pollQRCodeStatus(ctx context.Context, baseURL, qrcode string) (*wechatQRCodeStatusResponse, error) {
	targetURL, err := buildWechatBaseURL(baseURL, "ilink/bot/get_qrcode_status")
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("qrcode", qrcode)
	u.RawQuery = q.Encode()

	reqCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		reqCtx, cancel = context.WithTimeout(ctx, wechatQRStatusLongPollMS*time.Millisecond)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("iLink-App-ClientVersion", "1")
	resp, err := w.httpClient.Do(req)
	if err != nil {
		if reqCtx.Err() == context.DeadlineExceeded {
			return &wechatQRCodeStatusResponse{Status: "wait"}, nil
		}
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("wechat get_qrcode_status %d: %s", resp.StatusCode, string(raw))
	}
	var result wechatQRCodeStatusResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result.Status == "" {
		result.Status = "wait"
	}
	return &result, nil
}

func (w *WechatPersonalChannel) getUpdates(ctx context.Context, account *wechatAccountRuntime, getUpdatesBuf string) (*wechatGetUpdatesResponse, error) {
	timeout := account.pollTimeoutMS
	if timeout <= 0 {
		timeout = w.pollTimeoutMS
	}
	req := wechatGetUpdatesRequest{
		GetUpdatesBuf: getUpdatesBuf,
		BaseInfo:      wechatBaseInfo{ChannelVersion: wechatAPIChannelVersion},
	}
	var resp wechatGetUpdatesResponse
	if err := w.apiPost(ctx, account.baseURL, wechatAPIPathGetUpdates, account.token, req, timeout, &resp); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return &wechatGetUpdatesResponse{Ret: 0, GetUpdatesBuf: getUpdatesBuf}, nil
		}
		return nil, err
	}
	return &resp, nil
}

func (w *WechatPersonalChannel) sendMessage(ctx context.Context, account *wechatAccountRuntime, req *wechatSendMessageRequest) error {
	itemType := 0
	if req != nil && req.Msg != nil && len(req.Msg.ItemList) > 0 && req.Msg.ItemList[0] != nil {
		itemType = req.Msg.ItemList[0].Type
	}
	bodyJSON := ""
	if raw, err := json.Marshal(req); err == nil {
		bodyJSON = string(raw)
	} else {
		bodyJSON = fmt.Sprintf(`{"marshal_error":%q}`, err.Error())
	}
	w.Logger.Info("微信 sendmessage 请求",
		zap.String("account_id", account.id),
		zap.String("to_user_id", req.Msg.ToUserID),
		zap.String("item_type", wechatMessageItemTypeName(itemType)),
		zap.Int("item_count", len(req.Msg.ItemList)),
		zap.Bool("has_context_token", strings.TrimSpace(req.Msg.ContextToken) != ""),
		zap.String("body_json", bodyJSON),
	)
	var resp wechatSendMessageResponse
	if err := w.apiPost(ctx, account.baseURL, wechatAPIPathSendMessage, account.token, req, w.requestTimeoutMS, &resp); err != nil {
		w.Logger.Error("微信 sendmessage HTTP 失败",
			zap.String("account_id", account.id),
			zap.String("to_user_id", req.Msg.ToUserID),
			zap.String("item_type", wechatMessageItemTypeName(itemType)),
			zap.Error(err),
		)
		return err
	}
	if bizErr := resp.bizErr(); bizErr != nil {
		w.Logger.Error("微信 sendmessage 业务失败",
			zap.String("account_id", account.id),
			zap.String("to_user_id", req.Msg.ToUserID),
			zap.String("item_type", wechatMessageItemTypeName(itemType)),
			zap.Int("ret", resp.Ret),
			zap.Int("errcode", resp.ErrCode),
			zap.String("errmsg", resp.ErrMsg),
		)
		return fmt.Errorf("wechat sendmessage: %w", bizErr)
	}
	w.Logger.Info("微信 sendmessage 成功",
		zap.String("account_id", account.id),
		zap.String("to_user_id", req.Msg.ToUserID),
		zap.String("item_type", wechatMessageItemTypeName(itemType)),
	)
	return nil
}

func (w *WechatPersonalChannel) getUploadURL(ctx context.Context, account *wechatAccountRuntime, req *wechatGetUploadURLRequest) (*wechatGetUploadURLResponse, error) {
	w.Logger.Info("微信 getuploadurl 请求",
		zap.String("account_id", account.id),
		zap.String("to_user_id", req.ToUserID),
		zap.String("media_type", wechatMediaTypeName(req.MediaType)),
		zap.Int("raw_size", req.RawSize),
		zap.Int("file_size", req.FileSize),
		zap.Bool("no_need_thumb", req.NoNeedThumb),
	)
	var resp wechatGetUploadURLResponse
	if err := w.apiPost(ctx, account.baseURL, wechatAPIPathGetUploadURL, account.token, req, w.requestTimeoutMS, &resp); err != nil {
		w.Logger.Error("微信 getuploadurl 失败",
			zap.String("account_id", account.id),
			zap.String("to_user_id", req.ToUserID),
			zap.String("media_type", wechatMediaTypeName(req.MediaType)),
			zap.Error(err),
		)
		return nil, err
	}
	w.Logger.Info("微信 getuploadurl 响应",
		zap.String("account_id", account.id),
		zap.String("to_user_id", req.ToUserID),
		zap.String("media_type", wechatMediaTypeName(req.MediaType)),
		zap.Int("ret", resp.Ret),
		zap.Int("errcode", resp.ErrCode),
		zap.Bool("has_upload_param", strings.TrimSpace(resp.UploadParam) != ""),
		zap.Bool("has_upload_full_url", strings.TrimSpace(resp.UploadFullURL) != ""),
	)
	return &resp, nil
}

func (w *WechatPersonalChannel) getConfig(ctx context.Context, account *wechatAccountRuntime, userID, contextToken string) (*wechatGetConfigResponse, error) {
	var resp wechatGetConfigResponse
	err := w.apiPost(ctx, account.baseURL, wechatAPIPathGetConfig, account.token, wechatGetConfigRequest{
		ILinkUserID:  userID,
		ContextToken: contextToken,
		BaseInfo:     wechatBaseInfo{ChannelVersion: wechatAPIChannelVersion},
	}, w.configTimeoutMS, &resp)
	if err != nil {
		return nil, err
	}
	if bizErr := resp.bizErr(); bizErr != nil {
		return nil, bizErr
	}
	return &resp, nil
}

func (w *WechatPersonalChannel) getTypingTicket(ctx context.Context, account *wechatAccountRuntime, userID, contextToken string) (string, error) {
	if account == nil || userID == "" {
		return "", nil
	}
	now := time.Now()
	account.mu.RLock()
	entry := account.typingTickets[userID]
	account.mu.RUnlock()
	if entry != nil && !entry.NextFetchAt.IsZero() && now.Before(entry.NextFetchAt) {
		return entry.Ticket, nil
	}

	cfg, err := w.getConfig(ctx, account, userID, contextToken)
	if err == nil {
		ticket := strings.TrimSpace(cfg.TypingTicket)
		account.mu.Lock()
		if account.typingTickets == nil {
			account.typingTickets = make(map[string]*wechatTypingTicketState)
		}
		account.typingTickets[userID] = &wechatTypingTicketState{
			Ticket:        ticket,
			EverSucceeded: true,
			NextFetchAt:   now.Add(wechatTypingTicketTTL),
			RetryDelayS:   int(wechatTypingTicketInitialBackoff.Seconds()),
		}
		account.mu.Unlock()
		_ = w.saveAccountState(account)
		return ticket, nil
	}

	backoff := wechatTypingTicketInitialBackoff
	if entry != nil && entry.RetryDelayS > 0 {
		backoff = time.Duration(entry.RetryDelayS) * time.Second
		if backoff < wechatTypingTicketInitialBackoff {
			backoff = wechatTypingTicketInitialBackoff
		}
	}
	nextBackoff := backoff * 2
	if nextBackoff > wechatTypingTicketMaxBackoff {
		nextBackoff = wechatTypingTicketMaxBackoff
	}

	account.mu.Lock()
	if account.typingTickets == nil {
		account.typingTickets = make(map[string]*wechatTypingTicketState)
	}
	if entry == nil {
		entry = &wechatTypingTicketState{}
		account.typingTickets[userID] = entry
	}
	entry.NextFetchAt = now.Add(backoff)
	entry.RetryDelayS = int(nextBackoff.Seconds())
	ticket := entry.Ticket
	account.mu.Unlock()
	_ = w.saveAccountState(account)
	if ticket != "" {
		return ticket, nil
	}
	return "", err
}

func (w *WechatPersonalChannel) sendTyping(ctx context.Context, account *wechatAccountRuntime, userID, typingTicket string, status int) error {
	if typingTicket == "" || userID == "" {
		return nil
	}
	var resp wechatBaseResponse
	if err := w.apiPost(ctx, account.baseURL, wechatAPIPathSendTyping, account.token, wechatSendTypingRequest{
		ILinkUserID:  userID,
		TypingTicket: typingTicket,
		Status:       status,
		BaseInfo:     wechatBaseInfo{ChannelVersion: wechatAPIChannelVersion},
	}, w.configTimeoutMS, &resp); err != nil {
		return err
	}
	if bizErr := resp.bizErr(); bizErr != nil {
		return fmt.Errorf("wechat sendtyping: %w", bizErr)
	}
	return nil
}

func (w *WechatPersonalChannel) ensureTypingLoop(account *wechatAccountRuntime, userID, contextToken string) error {
	if account == nil || userID == "" {
		return nil
	}
	account.mu.RLock()
	existing := account.typingCancels[userID]
	account.mu.RUnlock()
	if existing != nil {
		return nil
	}

	ctx := w.rootCtx
	if ctx == nil {
		ctx = context.Background()
	}
	ticket, err := w.getTypingTicket(ctx, account, userID, contextToken)
	if err != nil || ticket == "" {
		return err
	}
	if err := w.sendTyping(ctx, account, userID, ticket, wechatTypingStatusTyping); err != nil {
		return err
	}

	loopCtx, cancel := context.WithCancel(ctx)
	loop := &wechatTypingLoop{cancel: cancel}
	account.mu.Lock()
	if account.typingCancels == nil {
		account.typingCancels = make(map[string]*wechatTypingLoop)
	}
	if old := account.typingCancels[userID]; old != nil {
		account.mu.Unlock()
		if old.cancel != nil {
			old.cancel()
		}
		account.mu.Lock()
	}
	account.typingCancels[userID] = loop
	account.mu.Unlock()

	go func() {
		ticker := time.NewTicker(wechatTypingKeepaliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				account.mu.Lock()
				if account.typingCancels[userID] == loop {
					delete(account.typingCancels, userID)
				}
				account.mu.Unlock()
				return
			case <-ticker.C:
				_ = w.sendTyping(loopCtx, account, userID, ticket, wechatTypingStatusTyping)
			}
		}
	}()
	return nil
}

func (w *WechatPersonalChannel) stopTypingLoop(account *wechatAccountRuntime, userID string, clearRemote bool) error {
	if account == nil || userID == "" {
		return nil
	}
	account.mu.Lock()
	loop := account.typingCancels[userID]
	delete(account.typingCancels, userID)
	entry := account.typingTickets[userID]
	account.mu.Unlock()
	if loop != nil && loop.cancel != nil {
		loop.cancel()
	}
	if !clearRemote || entry == nil || entry.Ticket == "" {
		return nil
	}
	timeout := w.configTimeoutMS
	if timeout <= 0 {
		timeout = wechatDefaultCfgTimeoutMS
	}
	ctx, cancelCtx := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Millisecond)
	defer cancelCtx()
	return w.sendTyping(ctx, account, userID, entry.Ticket, wechatTypingStatusCancel)
}
