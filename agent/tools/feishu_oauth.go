//go:build amd64 || arm64 || riscv64 || mips64 || mips64le || ppc64

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkauthen "github.com/larksuite/oapi-sdk-go/v3/service/authen/v1"
	"go.uber.org/zap"
)

// FeishuTokenData 持久化的 token 数据
type FeishuTokenData struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// FeishuTokenManager 管理飞书 OAuth2 用户 token 生命周期
type FeishuTokenManager struct {
	client    *lark.Client
	appID     string
	tokenFile string
	logger    *zap.Logger

	mu    sync.RWMutex
	token *FeishuTokenData
}

// FeishuTokenManagerConfig token manager 配置
type FeishuTokenManagerConfig struct {
	Client *lark.Client
	AppID  string
	Logger *zap.Logger
}

// NewFeishuTokenManager 创建 token manager
func NewFeishuTokenManager(cfg *FeishuTokenManagerConfig) *FeishuTokenManager {
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	home, _ := os.UserHomeDir()
	tokenFile := filepath.Join(home, ".pp-claw", "feishu_token.json")

	mgr := &FeishuTokenManager{
		client:    cfg.Client,
		appID:     cfg.AppID,
		tokenFile: tokenFile,
		logger:    logger,
	}

	// 加载已有 token
	mgr.loadToken()

	return mgr
}

// GetAuthURL 生成用户授权链接
func (m *FeishuTokenManager) GetAuthURL(redirectURL string) string {
	return fmt.Sprintf(
		"https://open.feishu.cn/open-apis/authen/v1/authorize?app_id=%s&redirect_uri=%s",
		m.appID, redirectURL,
	)
}

// GetUserToken 获取有效的 user_access_token，过期则自动刷新
func (m *FeishuTokenManager) GetUserToken(ctx context.Context) (string, error) {
	m.mu.RLock()
	token := m.token
	m.mu.RUnlock()

	if token == nil {
		return "", fmt.Errorf("no user token, need authorization")
	}

	// token 有效（预留 5 分钟缓冲）
	if time.Now().Add(5 * time.Minute).Before(token.ExpiresAt) {
		return token.AccessToken, nil
	}

	// refresh_token 已过期
	if time.Now().After(token.RefreshExpiresAt) {
		m.mu.Lock()
		m.token = nil
		m.mu.Unlock()
		return "", fmt.Errorf("refresh token expired, need re-authorization")
	}

	// 刷新 token
	return m.refreshToken(ctx, token.RefreshToken)
}

// HandleCallback 处理 OAuth 回调的 code，换取 token
func (m *FeishuTokenManager) HandleCallback(ctx context.Context, code string) error {
	req := larkauthen.NewCreateOidcAccessTokenReqBuilder().
		Body(larkauthen.NewCreateOidcAccessTokenReqBodyBuilder().
			GrantType("authorization_code").
			Code(code).
			Build()).
		Build()

	resp, err := m.client.Authen.OidcAccessToken.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("exchange code for token failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("exchange code for token failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	data := resp.Data
	tokenData := &FeishuTokenData{
		AccessToken:  ptrStr(data.AccessToken),
		RefreshToken: ptrStr(data.RefreshToken),
	}
	if data.ExpiresIn != nil {
		tokenData.ExpiresAt = time.Now().Add(time.Duration(*data.ExpiresIn) * time.Second)
	}
	if data.RefreshExpiresIn != nil {
		tokenData.RefreshExpiresAt = time.Now().Add(time.Duration(*data.RefreshExpiresIn) * time.Second)
	}

	m.mu.Lock()
	m.token = tokenData
	m.mu.Unlock()

	m.saveToken(tokenData)
	m.logger.Info("Feishu user token obtained",
		zap.Time("expires_at", tokenData.ExpiresAt),
		zap.Time("refresh_expires_at", tokenData.RefreshExpiresAt),
	)

	return nil
}

// ServeHTTP 实现 http.HandlerFunc，供外部 HTTP server 挂载
func (m *FeishuTokenManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code parameter", http.StatusBadRequest)
		return
	}

	if err := m.HandleCallback(r.Context(), code); err != nil {
		m.logger.Error("Feishu OAuth callback failed", zap.Error(err))
		http.Error(w, fmt.Sprintf("Authorization failed: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html><html><body style="text-align:center;padding-top:50px;font-family:sans-serif;">
<h2>✅ 飞书授权成功</h2><p>你可以关闭此页面，回到 pp-claw 继续使用搜索功能。</p></body></html>`)
}

// HasToken 检查是否已有 token
func (m *FeishuTokenManager) HasToken() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.token != nil
}

// refreshToken 使用 refresh_token 刷新 access_token
func (m *FeishuTokenManager) refreshToken(ctx context.Context, refreshToken string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 双检：可能已被其他 goroutine 刷新
	if m.token != nil && time.Now().Add(5*time.Minute).Before(m.token.ExpiresAt) {
		return m.token.AccessToken, nil
	}

	req := larkauthen.NewCreateOidcRefreshAccessTokenReqBuilder().
		Body(larkauthen.NewCreateOidcRefreshAccessTokenReqBodyBuilder().
			GrantType("refresh_token").
			RefreshToken(refreshToken).
			Build()).
		Build()

	resp, err := m.client.Authen.OidcRefreshAccessToken.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("refresh token failed: %w", err)
	}
	if !resp.Success() {
		m.token = nil
		return "", fmt.Errorf("refresh token failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	data := resp.Data
	tokenData := &FeishuTokenData{
		AccessToken:  ptrStr(data.AccessToken),
		RefreshToken: ptrStr(data.RefreshToken),
	}
	if data.ExpiresIn != nil {
		tokenData.ExpiresAt = time.Now().Add(time.Duration(*data.ExpiresIn) * time.Second)
	}
	if data.RefreshExpiresIn != nil {
		tokenData.RefreshExpiresAt = time.Now().Add(time.Duration(*data.RefreshExpiresIn) * time.Second)
	}

	m.token = tokenData
	m.saveToken(tokenData)
	m.logger.Info("Feishu user token refreshed", zap.Time("expires_at", tokenData.ExpiresAt))

	return tokenData.AccessToken, nil
}

// loadToken 从文件加载 token
func (m *FeishuTokenManager) loadToken() {
	data, err := os.ReadFile(m.tokenFile)
	if err != nil {
		return
	}

	var token FeishuTokenData
	if err := json.Unmarshal(data, &token); err != nil {
		m.logger.Warn("Failed to parse feishu token file", zap.Error(err))
		return
	}

	// refresh_token 已过期则不加载
	if time.Now().After(token.RefreshExpiresAt) {
		m.logger.Info("Feishu token expired, need re-authorization")
		return
	}

	m.token = &token
	m.logger.Info("Feishu user token loaded from file",
		zap.Time("expires_at", token.ExpiresAt),
		zap.Time("refresh_expires_at", token.RefreshExpiresAt),
	)
}

// saveToken 持久化 token 到文件
func (m *FeishuTokenManager) saveToken(token *FeishuTokenData) {
	dir := filepath.Dir(m.tokenFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		m.logger.Error("Failed to create token dir", zap.Error(err))
		return
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		m.logger.Error("Failed to marshal token", zap.Error(err))
		return
	}

	if err := os.WriteFile(m.tokenFile, data, 0600); err != nil {
		m.logger.Error("Failed to save token file", zap.Error(err))
	}
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
