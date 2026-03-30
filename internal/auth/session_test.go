package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
)

func TestSessionLoginAndLoadCachedToken(t *testing.T) {
	t.Parallel()

	var (
		pollCount int
		server    *httptest.Server
	)

	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/organizations/v2.0/.well-known/openid-configuration" {
			_, _ = io.WriteString(w, fmt.Sprintf(
				`{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"device_authorization_endpoint":%q}`,
				server.URL+"/organizations/v2.0",
				server.URL+"/organizations/oauth2/v2.0/authorize",
				server.URL+"/organizations/oauth2/v2.0/token",
				server.URL+"/organizations/oauth2/v2.0/devicecode",
			))
			return
		}

		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		if r.URL.Path == "/organizations/oauth2/v2.0/devicecode" {
			_, _ = io.WriteString(w, `{"device_code":"dev-1","user_code":"CODE-1","verification_uri":"https://microsoft.com/devicelogin","expires_in":900,"interval":0,"message":"请访问示例链接登录"}`)
			return
		}

		if r.URL.Path != "/organizations/oauth2/v2.0/token" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse query: %v", err)
		}

		switch values.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code", "device_code":
			pollCount++
			if pollCount == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"authorization_pending","error_description":"pending"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"access-1","refresh_token":"refresh-1","token_type":"Bearer","scope":"scope-a scope-ews offline_access","expires_in":3600}`)
		default:
			t.Fatalf("unexpected grant_type: %s", values.Get("grant_type"))
		}
	}))
	defer server.Close()

	cfg := appconfig.AuthConfig{
		ClientID:         "client-id",
		Tenant:           "organizations",
		AuthorityBaseURL: server.URL,
		GraphScopes:      []string{"scope-a", "offline_access"},
		EWSScopes:        []string{"scope-ews"},
		StateDir:         t.TempDir(),
	}

	session, err := NewSession(cfg, server.Client())
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	if _, err := session.Login(context.Background(), io.Discard); err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	token, err := session.LoadCachedToken()
	if err != nil {
		t.Fatalf("LoadCachedToken() error = %v", err)
	}
	if token.AccessToken != "access-1" {
		t.Fatalf("AccessToken = %q, want access-1", token.AccessToken)
	}

	accessToken, err := session.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken() error = %v", err)
	}
	if accessToken != "access-1" {
		t.Fatalf("unexpected access token: %s", accessToken)
	}
}

func TestLoadConfigFromEnvCompatibleScopes(t *testing.T) {
	t.Setenv("MIMECRYPT_CLIENT_ID", "client-id")
	t.Setenv("MIMECRYPT_GRAPH_SCOPES", "scope-a offline_access")
	t.Setenv("MIMECRYPT_EWS_SCOPES", "scope-ews")
	t.Setenv("MIMECRYPT_STATE_DIR", t.TempDir())

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	if cfg.Auth.ClientID != "client-id" {
		t.Fatalf("unexpected client id: %s", cfg.Auth.ClientID)
	}
	if got := strings.Join(cfg.Auth.GraphScopes, " "); got != "scope-a offline_access" {
		t.Fatalf("unexpected graph scopes: %s", got)
	}
	if got := strings.Join(cfg.Auth.EWSScopes, " "); got != "scope-ews" {
		t.Fatalf("unexpected ews scopes: %s", got)
	}
}

func TestSessionAccessTokenForScopesRefreshesWhenCachedScopesDoNotCoverRequest(t *testing.T) {
	t.Parallel()

	var refreshScope string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/organizations/oauth2/v2.0/token" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("ParseQuery() error = %v", err)
		}
		refreshScope = values.Get("scope")
		_, _ = io.WriteString(w, `{"access_token":"access-ews","refresh_token":"refresh-2","token_type":"Bearer","scope":"scope-ews","expires_in":3600}`)
	}))
	defer server.Close()

	cfg := appconfig.AuthConfig{
		ClientID:         "client-id",
		Tenant:           "organizations",
		AuthorityBaseURL: server.URL,
		GraphScopes:      []string{"scope-a", "offline_access"},
		EWSScopes:        []string{"scope-ews"},
		StateDir:         t.TempDir(),
	}

	session, err := NewSession(cfg, server.Client())
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := session.store.save(Token{
		AccessToken:  "access-graph",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Scope:        "scope-a",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("save() error = %v", err)
	}

	accessToken, err := session.AccessTokenForScopes(context.Background(), cfg.EWSScopes)
	if err != nil {
		t.Fatalf("AccessTokenForScopes() error = %v", err)
	}
	if accessToken != "access-ews" {
		t.Fatalf("access token = %q, want access-ews", accessToken)
	}
	if refreshScope != "scope-ews" {
		t.Fatalf("refresh scope = %q, want scope-ews", refreshScope)
	}
}

func TestSessionAccessTokenForScopesSerializesRefreshAcrossSessions(t *testing.T) {
	t.Parallel()

	var refreshCalls atomic.Int32
	requestStarted := make(chan struct{}, 2)
	allowResponse := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/organizations/oauth2/v2.0/token" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Fatalf("grant_type = %q, want refresh_token", got)
		}
		refreshCalls.Add(1)
		requestStarted <- struct{}{}
		<-allowResponse
		_, _ = io.WriteString(w, `{"access_token":"access-refresh","refresh_token":"refresh-next","token_type":"Bearer","scope":"scope-a","expires_in":3600}`)
	}))
	defer server.Close()

	cfg := appconfig.AuthConfig{
		ClientID:         "client-id",
		Tenant:           "organizations",
		AuthorityBaseURL: server.URL,
		GraphScopes:      []string{"scope-a", "offline_access"},
		StateDir:         t.TempDir(),
	}

	sessionA, err := NewSession(cfg, server.Client())
	if err != nil {
		t.Fatalf("NewSession(sessionA) error = %v", err)
	}
	sessionB, err := NewSession(cfg, server.Client())
	if err != nil {
		t.Fatalf("NewSession(sessionB) error = %v", err)
	}
	if err := sessionA.store.save(Token{
		AccessToken:  "stale-access",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Scope:        "scope-a",
		ExpiresAt:    time.Now().Add(30 * time.Second),
	}); err != nil {
		t.Fatalf("save() error = %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	for _, session := range []*Session{sessionA, sessionB} {
		session := session
		go func() {
			defer wg.Done()
			<-start
			token, err := session.AccessToken(context.Background())
			if err != nil {
				errs <- err
				return
			}
			if token != "access-refresh" {
				errs <- fmt.Errorf("unexpected access token: %s", token)
			}
		}()
	}
	close(start)

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for first refresh request")
	}

	select {
	case <-requestStarted:
		t.Fatalf("observed a second concurrent refresh request")
	case <-time.After(200 * time.Millisecond):
	}

	close(allowResponse)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("AccessToken() error = %v", err)
		}
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func TestSessionIgnoresLegacyTokenPath(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	cfg := appconfig.AuthConfig{
		ClientID:         "client-id",
		Tenant:           "organizations",
		AuthorityBaseURL: "https://login.microsoftonline.com",
		IMAPScopes:       []string{"scope-imap"},
		StateDir:         stateDir,
	}

	session, err := NewSession(cfg, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	legacyPath := filepath.Join(stateDir, "graph-token.json")
	if err := os.WriteFile(legacyPath, []byte(`{"access_token":"legacy"}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err = session.LoadCachedToken()
	if err == nil || !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("LoadCachedToken() error = %v, want ErrLoginRequired", err)
	}
}

func TestSessionLogoutIgnoresMissingToken(t *testing.T) {
	t.Parallel()

	session, err := NewSession(appconfig.AuthConfig{
		ClientID:         "client-id",
		Tenant:           "organizations",
		AuthorityBaseURL: "https://login.microsoftonline.com",
		IMAPScopes:       []string{"scope-imap"},
		StateDir:         t.TempDir(),
	}, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	if err := session.Logout(); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
}
