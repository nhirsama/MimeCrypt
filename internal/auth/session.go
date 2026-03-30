package auth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	msalcache "github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

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

type legacyClient struct {
	httpClient *http.Client
	cfg        appconfig.AuthConfig
	now        func() time.Time
}

type Session struct {
	cfg          appconfig.AuthConfig
	client       public.Client
	legacyClient *legacyClient
	store        *tokenStore
}

var _ provider.Session = (*Session)(nil)

var sessionStoreLocks sync.Map

// NewSession 创建一个只负责登录与 token 缓存的会话对象。
func NewSession(cfg appconfig.AuthConfig, httpClient *http.Client) (*Session, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if httpClient == nil {
		httpClient = defaultSessionHTTPClient(cfg)
	}

	store, err := newTokenStore(cfg)
	if err != nil {
		return nil, err
	}

	client, err := newMSALClient(cfg, httpClient, &msalCacheAccessor{store: store})
	if err != nil {
		return nil, err
	}

	return &Session{
		cfg:    cfg,
		client: client,
		legacyClient: &legacyClient{
			httpClient: httpClient,
			cfg:        cfg,
			now:        time.Now,
		},
		store: store,
	}, nil
}

func defaultSessionHTTPClient(cfg appconfig.AuthConfig) *http.Client {
	transport := http.DefaultTransport
	if loopbackTLSAuthority(cfg.AuthorityBaseURL) {
		if base, ok := http.DefaultTransport.(*http.Transport); ok {
			cloned := base.Clone()
			if cloned.TLSClientConfig == nil {
				cloned.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			} else {
				cloned.TLSClientConfig = cloned.TLSClientConfig.Clone()
			}
			cloned.TLSClientConfig.InsecureSkipVerify = true
			transport = cloned
		}
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
}

func loopbackTLSAuthority(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Login 通过 device code 引导用户完成登录，并把 token 保存到本地缓存。
func (s *Session) Login(ctx context.Context, out io.Writer) (Token, error) {
	if out == nil {
		out = io.Discard
	}

	deviceCode, err := s.client.AcquireTokenByDeviceCode(ctx, consentScopes(s.cfg.GraphScopes, s.cfg.EWSScopes, s.cfg.IMAPScopes))
	if err != nil {
		return Token{}, fmt.Errorf("发起 device code 登录失败: %w", err)
	}

	if strings.TrimSpace(deviceCode.Result.Message) != "" {
		if _, err := fmt.Fprintln(out, deviceCode.Result.Message); err != nil {
			return Token{}, fmt.Errorf("输出登录提示失败: %w", err)
		}
	} else {
		if _, err := fmt.Fprintf(out, "请访问 %s 并输入验证码 %s 完成登录。\n", deviceCode.Result.VerificationURL, deviceCode.Result.UserCode); err != nil {
			return Token{}, fmt.Errorf("输出登录提示失败: %w", err)
		}
	}

	result, err := deviceCode.AuthenticationResult(ctx)
	if err != nil {
		return Token{}, fmt.Errorf("等待用户完成登录失败: %w", err)
	}

	release, err := acquireSessionStoreGuard(s)
	if err != nil {
		return Token{}, err
	}
	defer release()

	token := authResultToToken(result)
	if err := s.persistMSALToken(token); err != nil {
		return Token{}, err
	}
	return token, nil
}

// AccessToken 返回可直接用于 Graph 调用的 access token，必要时会自动刷新。
func (s *Session) AccessToken(ctx context.Context) (string, error) {
	return s.AccessTokenForScopes(ctx, defaultAccessScopes(s.cfg))
}

// AccessTokenForScopes 返回满足指定 scopes 的 access token，必要时会自动刷新。
func (s *Session) AccessTokenForScopes(ctx context.Context, scopes []string) (string, error) {
	release, err := acquireSessionStoreGuard(s)
	if err != nil {
		return "", err
	}
	defer release()

	scopes = normalizeScopes(scopes)
	if len(scopes) == 0 {
		return "", fmt.Errorf("scope 不能为空")
	}

	if account, ok, err := s.currentAccount(ctx); err != nil {
		return "", err
	} else if ok {
		result, err := s.client.AcquireTokenSilent(ctx, scopes, public.WithSilentAccount(account))
		if err == nil {
			token := authResultToToken(result)
			if err := s.persistMSALToken(token); err != nil {
				return "", err
			}
			return token.AccessToken, nil
		}
		legacyAccessToken, legacyOK, legacyErr := s.accessTokenFromLegacyRecord(ctx, scopes)
		if legacyErr == nil && legacyOK {
			return legacyAccessToken, nil
		}
		if legacyErr != nil {
			return "", legacyErr
		}
		return "", fmt.Errorf("获取 access token 失败: %w", err)
	}

	legacyAccessToken, legacyOK, legacyErr := s.accessTokenFromLegacyRecord(ctx, scopes)
	if legacyErr != nil {
		return "", legacyErr
	}
	if legacyOK {
		return legacyAccessToken, nil
	}
	return "", fmt.Errorf("%w，请先执行 login", ErrLoginRequired)
}

// LoadCachedToken 读取本地缓存 token，便于调试和状态检查。
func (s *Session) LoadCachedToken() (Token, error) {
	release, err := acquireSessionStoreGuard(s)
	if err != nil {
		return Token{}, err
	}
	defer release()

	record, err := s.store.loadRecord()
	if err != nil {
		return Token{}, err
	}
	if !tokenEmpty(record.Token) {
		return record.Token, nil
	}
	if len(record.Cache) > 0 {
		return Token{
			TokenType: "Bearer",
			Scope:     strings.Join(consentScopes(s.cfg.GraphScopes, s.cfg.EWSScopes, s.cfg.IMAPScopes), " "),
		}, nil
	}
	return Token{}, fmt.Errorf("%w，请先执行 login", ErrLoginRequired)
}

// StoreToken 将外部提供的 token 写入当前配置对应的存储后端。
func (s *Session) StoreToken(token Token) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("token 存储未初始化")
	}
	if strings.TrimSpace(token.AccessToken) == "" && strings.TrimSpace(token.RefreshToken) == "" {
		return fmt.Errorf("导入 token 失败: access token 和 refresh token 不能同时为空")
	}
	release, err := acquireSessionStoreGuard(s)
	if err != nil {
		return err
	}
	defer release()
	return s.store.save(token)
}

// Logout 清除本地 token 缓存。
func (s *Session) Logout() error {
	release, err := acquireSessionStoreGuard(s)
	if err != nil {
		return err
	}
	defer release()
	if err := s.store.delete(); err != nil {
		if errors.Is(err, errTokenNotFound) {
			return nil
		}
		return err
	}
	return nil
}

func sessionStoreLock(s *Session) *sync.Mutex {
	identity := "session:unknown"
	if s != nil && s.store != nil && strings.TrimSpace(s.store.identity) != "" {
		identity = s.store.identity
	}
	lock, _ := sessionStoreLocks.LoadOrStore(identity, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func acquireSessionStoreGuard(s *Session) (func(), error) {
	lock := sessionStoreLock(s)
	lock.Lock()

	release := func() {
		lock.Unlock()
	}

	if s == nil || s.store == nil || strings.TrimSpace(s.store.lockPath) == "" {
		return release, nil
	}

	fileLock, err := acquireTokenStoreFileLock(s.store.lockPath)
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	return func() {
		_ = fileLock.Release()
		lock.Unlock()
	}, nil
}

func newMSALClient(cfg appconfig.AuthConfig, httpClient *http.Client, accessor msalcache.ExportReplace) (public.Client, error) {
	authority, err := authorityURL(cfg)
	if err != nil {
		return public.Client{}, err
	}
	client, err := public.New(
		cfg.ClientID,
		public.WithAuthority(authority),
		public.WithCache(accessor),
		public.WithHTTPClient(httpClient),
		public.WithInstanceDiscovery(false),
	)
	if err != nil {
		return public.Client{}, fmt.Errorf("初始化认证客户端失败: %w", err)
	}
	return client, nil
}

func authorityURL(cfg appconfig.AuthConfig) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.AuthorityBaseURL), "/")
	tenant := strings.TrimSpace(cfg.Tenant)
	if base == "" {
		return "", fmt.Errorf("authority base URL 不能为空")
	}
	if tenant == "" {
		return "", fmt.Errorf("tenant 不能为空")
	}
	return base + "/" + url.PathEscape(tenant), nil
}

func (s *Session) currentAccount(ctx context.Context) (public.Account, bool, error) {
	accounts, err := s.client.Accounts(ctx)
	if err != nil {
		return public.Account{}, false, fmt.Errorf("读取本地登录账号失败: %w", err)
	}
	if len(accounts) == 0 {
		return public.Account{}, false, nil
	}
	return accounts[0], true, nil
}

func (s *Session) accessTokenFromLegacyRecord(ctx context.Context, scopes []string) (string, bool, error) {
	record, found, err := s.store.loadRecordIfExists()
	if err != nil {
		return "", false, err
	}
	if !found || tokenEmpty(record.Token) {
		return "", false, nil
	}

	token := record.Token
	if token.AccessToken != "" && time.Until(token.ExpiresAt) > 2*time.Minute && tokenCoversScopes(token.Scope, scopes) {
		return token.AccessToken, true, nil
	}
	if token.RefreshToken == "" {
		return "", false, nil
	}

	refreshed, err := s.legacyClient.refreshTokenForScopes(ctx, token.RefreshToken, scopes)
	if err != nil {
		return "", false, err
	}
	if err := s.store.save(refreshed); err != nil {
		return "", false, err
	}
	return refreshed.AccessToken, true, nil
}

func (s *Session) persistMSALToken(token Token) error {
	record, found, err := s.store.loadRecordIfExists()
	if err != nil {
		return err
	}
	if !found {
		record = sessionRecord{}
	}
	record.Format = sessionRecordFormatMSAL
	record.Token = token
	return s.store.saveRecord(record)
}

func authResultToToken(result public.AuthResult) Token {
	return Token{
		AccessToken: result.AccessToken,
		TokenType:   "Bearer",
		Scope:       strings.Join(normalizeScopes(result.GrantedScopes), " "),
		ExpiresAt:   result.ExpiresOn,
	}
}

type msalCacheAccessor struct {
	store *tokenStore
}

func (a *msalCacheAccessor) Replace(_ context.Context, cache msalcache.Unmarshaler, _ msalcache.ReplaceHints) error {
	if a == nil || a.store == nil || cache == nil {
		return nil
	}
	record, found, err := a.store.loadRecordIfExists()
	if err != nil || !found || len(record.Cache) == 0 {
		return err
	}
	return cache.Unmarshal(record.Cache)
}

func (a *msalCacheAccessor) Export(_ context.Context, cache msalcache.Marshaler, _ msalcache.ExportHints) error {
	if a == nil || a.store == nil || cache == nil {
		return nil
	}
	content, err := cache.Marshal()
	if err != nil {
		return err
	}
	record, found, err := a.store.loadRecordIfExists()
	if err != nil {
		return err
	}
	if !found {
		record = sessionRecord{}
	}
	record.Format = sessionRecordFormatMSAL
	record.Cache = append(record.Cache[:0], content...)
	return a.store.saveRecord(record)
}

func (c *legacyClient) refreshTokenForScopes(ctx context.Context, refreshToken string, scopes []string) (Token, error) {
	form := url.Values{}
	form.Set("client_id", c.cfg.ClientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("scope", strings.Join(normalizeScopes(scopes), " "))

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

func (c *legacyClient) doTokenRequest(ctx context.Context, form url.Values) (tokenResponse, int, error) {
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

func (c *legacyClient) toToken(payload tokenResponse) Token {
	return Token{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		TokenType:    payload.TokenType,
		Scope:        payload.Scope,
		ExpiresAt:    c.now().Add(time.Duration(payload.ExpiresIn) * time.Second),
	}
}

func consentScopes(groups ...[]string) []string {
	seen := make(map[string]struct{})
	scopes := make([]string, 0)
	for _, group := range groups {
		for _, scope := range group {
			scope = strings.TrimSpace(scope)
			if scope == "" {
				continue
			}
			if _, ok := seen[scope]; ok {
				continue
			}
			seen[scope] = struct{}{}
			scopes = append(scopes, scope)
		}
	}
	slices.Sort(scopes)
	return scopes
}

func normalizeScopes(scopes []string) []string {
	result := consentScopes(scopes)
	if len(result) == 0 {
		return nil
	}
	return result
}

func defaultAccessScopes(cfg appconfig.AuthConfig) []string {
	switch {
	case len(cfg.GraphScopes) > 0:
		return cfg.GraphScopes
	case len(cfg.IMAPScopes) > 0:
		return cfg.IMAPScopes
	case len(cfg.EWSScopes) > 0:
		return cfg.EWSScopes
	default:
		return nil
	}
}

func tokenCoversScopes(granted string, requested []string) bool {
	grantedSet := make(map[string]struct{})
	for _, scope := range strings.Fields(granted) {
		scope = strings.TrimSpace(scope)
		if scope == "" || ignoredTokenScope(scope) {
			continue
		}
		grantedSet[scope] = struct{}{}
	}

	for _, scope := range requested {
		scope = strings.TrimSpace(scope)
		if scope == "" || ignoredTokenScope(scope) {
			continue
		}
		if _, ok := grantedSet[scope]; !ok {
			return false
		}
	}

	return true
}

func ignoredTokenScope(scope string) bool {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "offline_access", "openid", "profile":
		return true
	default:
		return false
	}
}
