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
	return w.apiPost(ctx, account.baseURL, wechatAPIPathSendMessage, account.token, req, w.requestTimeoutMS, nil)
}

func (w *WechatPersonalChannel) getUploadURL(ctx context.Context, account *wechatAccountRuntime, req *wechatGetUploadURLRequest) (*wechatGetUploadURLResponse, error) {
	var resp wechatGetUploadURLResponse
	if err := w.apiPost(ctx, account.baseURL, wechatAPIPathGetUploadURL, account.token, req, w.requestTimeoutMS, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (w *WechatPersonalChannel) getConfig(ctx context.Context, account *wechatAccountRuntime, contextToken string) (*wechatGetConfigResponse, error) {
	var resp wechatGetConfigResponse
	err := w.apiPost(ctx, account.baseURL, wechatAPIPathGetConfig, account.token, wechatGetConfigRequest{
		ILinkUserID:  account.ilinkUserID,
		ContextToken: contextToken,
		BaseInfo:     wechatBaseInfo{ChannelVersion: wechatAPIChannelVersion},
	}, w.configTimeoutMS, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (w *WechatPersonalChannel) sendTyping(ctx context.Context, account *wechatAccountRuntime, status int, contextToken string) error {
	account.mu.RLock()
	typingTicket := account.typingTicket
	account.mu.RUnlock()

	if typingTicket == "" {
		cfg, err := w.getConfig(ctx, account, contextToken)
		if err != nil {
			return err
		}
		typingTicket = cfg.TypingTicket
		account.mu.Lock()
		account.typingTicket = typingTicket
		account.mu.Unlock()
		_ = w.saveAccountState(account)
	}
	if typingTicket == "" || account.ilinkUserID == "" {
		return nil
	}
	return w.apiPost(ctx, account.baseURL, wechatAPIPathSendTyping, account.token, wechatSendTypingRequest{
		ILinkUserID:  account.ilinkUserID,
		TypingTicket: typingTicket,
		Status:       status,
		BaseInfo:     wechatBaseInfo{ChannelVersion: wechatAPIChannelVersion},
	}, w.configTimeoutMS, nil)
}
