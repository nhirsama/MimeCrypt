package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

type DeviceCode struct {
	DeviceCode       string `json:"device_code"`
	UserCode         string `json:"user_code"`
	VerificationURI  string `json:"verification_uri"`
	ExpiresIn        int    `json:"expires_in"`
	Interval         int    `json:"interval"`
	Message          string `json:"message"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type Token = provider.Token

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type client struct {
	httpClient *http.Client
	cfg        appconfig.AuthConfig
	now        func() time.Time
}

type tokenStore struct {
	path string
}

type Session struct {
	client *client
	store  *tokenStore
}

var _ provider.Session = (*Session)(nil)

// NewSession 创建一个只负责登录与 token 缓存的会话对象。
func NewSession(cfg appconfig.AuthConfig, httpClient *http.Client) (*Session, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &Session{
		client: &client{
			httpClient: httpClient,
			cfg:        cfg,
			now:        time.Now,
		},
		store: &tokenStore{path: cfg.TokenPath()},
	}, nil
}

// Login 通过 device code 引导用户完成登录，并把 token 保存到本地缓存。
func (s *Session) Login(ctx context.Context, out io.Writer) (Token, error) {
	deviceCode, err := s.client.startDeviceCode(ctx)
	if err != nil {
		return Token{}, err
	}

	if deviceCode.Message != "" {
		if _, err := fmt.Fprintln(out, deviceCode.Message); err != nil {
			return Token{}, fmt.Errorf("输出登录提示失败: %w", err)
		}
	} else {
		if _, err := fmt.Fprintf(out, "请访问 %s 并输入验证码 %s 完成登录。\n", deviceCode.VerificationURI, deviceCode.UserCode); err != nil {
			return Token{}, fmt.Errorf("输出登录提示失败: %w", err)
		}
	}

	token, err := s.client.waitForToken(ctx, deviceCode)
	if err != nil {
		return Token{}, err
	}

	if err := s.store.save(token); err != nil {
		return Token{}, err
	}

	return token, nil
}

// AccessToken 返回可直接用于 Graph 调用的 access token，必要时会自动刷新。
func (s *Session) AccessToken(ctx context.Context) (string, error) {
	token, err := s.store.load()
	if err != nil {
		return "", err
	}

	if token.AccessToken != "" && time.Until(token.ExpiresAt) > 2*time.Minute {
		return token.AccessToken, nil
	}
	if token.RefreshToken == "" {
		return "", fmt.Errorf("本地 token 缓存中没有 refresh token，请先执行 login")
	}

	refreshed, err := s.client.refreshToken(ctx, token.RefreshToken)
	if err != nil {
		return "", err
	}

	if err := s.store.save(refreshed); err != nil {
		return "", err
	}

	return refreshed.AccessToken, nil
}

// LoadCachedToken 读取本地缓存 token，便于调试和状态检查。
func (s *Session) LoadCachedToken() (Token, error) {
	return s.store.load()
}

// Logout 清除本地 token 缓存。
func (s *Session) Logout() error {
	return s.store.delete()
}

func (c *client) startDeviceCode(ctx context.Context) (DeviceCode, error) {
	form := url.Values{}
	form.Set("client_id", c.cfg.ClientID)
	form.Set("scope", strings.Join(c.cfg.GraphScopes, " "))

	endpoint := fmt.Sprintf(
		"%s/%s/oauth2/v2.0/devicecode",
		strings.TrimRight(c.cfg.AuthorityBaseURL, "/"),
		url.PathEscape(c.cfg.Tenant),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return DeviceCode{}, fmt.Errorf("构造 device code 请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return DeviceCode{}, fmt.Errorf("执行 device code 请求失败: %w", err)
	}
	defer resp.Body.Close()

	var deviceCode DeviceCode
	if err := json.NewDecoder(resp.Body).Decode(&deviceCode); err != nil {
		return DeviceCode{}, fmt.Errorf("解析 device code 响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if deviceCode.Error != "" {
			return DeviceCode{}, fmt.Errorf("device code 接口返回异常状态: %s: %s", deviceCode.Error, deviceCode.ErrorDescription)
		}
		return DeviceCode{}, fmt.Errorf("device code 接口返回异常状态: %s", resp.Status)
	}
	if deviceCode.DeviceCode == "" {
		return DeviceCode{}, fmt.Errorf("device code 响应中缺少 device_code")
	}

	return deviceCode, nil
}

func (c *client) waitForToken(ctx context.Context, deviceCode DeviceCode) (Token, error) {
	interval := time.Duration(deviceCode.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}

	for {
		token, pending, nextInterval, err := c.pollDeviceToken(ctx, deviceCode.DeviceCode)
		if err != nil {
			return Token{}, err
		}
		if !pending {
			return token, nil
		}
		if nextInterval > 0 {
			interval = nextInterval
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Token{}, fmt.Errorf("等待用户登录超时或被取消: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (c *client) refreshToken(ctx context.Context, refreshToken string) (Token, error) {
	form := url.Values{}
	form.Set("client_id", c.cfg.ClientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("scope", strings.Join(c.cfg.GraphScopes, " "))

	payload, status, err := c.doTokenRequest(ctx, form)
	if err != nil {
		return Token{}, err
	}
	if status != http.StatusOK {
		if payload.Error != "" {
			return Token{}, fmt.Errorf("刷新 token 失败: %s: %s", payload.Error, payload.ErrorDescription)
		}
		return Token{}, fmt.Errorf("刷新 token 失败，状态码: %d", status)
	}

	token := c.toToken(payload)
	if token.RefreshToken == "" {
		token.RefreshToken = refreshToken
	}

	return token, nil
}

func (c *client) pollDeviceToken(ctx context.Context, deviceCode string) (Token, bool, time.Duration, error) {
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	form.Set("client_id", c.cfg.ClientID)
	form.Set("device_code", deviceCode)

	payload, status, err := c.doTokenRequest(ctx, form)
	if err != nil {
		return Token{}, false, 0, err
	}

	if status == http.StatusOK {
		return c.toToken(payload), false, 0, nil
	}

	switch payload.Error {
	case "authorization_pending":
		return Token{}, true, 0, nil
	case "slow_down":
		return Token{}, true, 10 * time.Second, nil
	case "authorization_declined":
		return Token{}, false, 0, fmt.Errorf("用户拒绝了授权请求")
	case "expired_token":
		return Token{}, false, 0, fmt.Errorf("device code 已过期，请重新执行 login")
	case "bad_verification_code":
		return Token{}, false, 0, fmt.Errorf("device code 无效，请重新执行 login")
	default:
		if payload.Error != "" {
			return Token{}, false, 0, fmt.Errorf("轮询 token 失败: %s: %s", payload.Error, payload.ErrorDescription)
		}
		return Token{}, false, 0, fmt.Errorf("轮询 token 失败，状态码: %d", status)
	}
}

func (c *client) doTokenRequest(ctx context.Context, form url.Values) (tokenResponse, int, error) {
	endpoint := fmt.Sprintf(
		"%s/%s/oauth2/v2.0/token",
		strings.TrimRight(c.cfg.AuthorityBaseURL, "/"),
		url.PathEscape(c.cfg.Tenant),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, 0, fmt.Errorf("构造 token 请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return tokenResponse{}, 0, fmt.Errorf("执行 token 请求失败: %w", err)
	}
	defer resp.Body.Close()

	var payload tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return tokenResponse{}, 0, fmt.Errorf("解析 token 响应失败: %w", err)
	}

	return payload, resp.StatusCode, nil
}

func (c *client) toToken(payload tokenResponse) Token {
	return Token{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		TokenType:    payload.TokenType,
		Scope:        payload.Scope,
		ExpiresAt:    c.now().Add(time.Duration(payload.ExpiresIn) * time.Second),
	}
}

func (s *tokenStore) load() (Token, error) {
	if s.path == "" {
		return Token{}, fmt.Errorf("token 路径不能为空")
	}

	content, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Token{}, fmt.Errorf("未找到登录状态，请先执行 login")
		}
		return Token{}, fmt.Errorf("读取 token 缓存失败: %w", err)
	}

	var token Token
	if err := json.Unmarshal(content, &token); err != nil {
		return Token{}, fmt.Errorf("解析 token 缓存失败: %w", err)
	}

	return token, nil
}

func (s *tokenStore) save(token Token) error {
	if s.path == "" {
		return fmt.Errorf("token 路径不能为空")
	}

	return writeJSONFile(s.path, token, 0o600)
}

func (s *tokenStore) delete() error {
	if s.path == "" {
		return fmt.Errorf("token 路径不能为空")
	}

	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除 token 缓存失败: %w", err)
	}

	return nil
}

func writeJSONFile(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建状态目录失败: %w", err)
	}

	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, mode); err != nil {
		return fmt.Errorf("写入临时状态文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("替换状态文件失败: %w", err)
	}

	return nil
}
