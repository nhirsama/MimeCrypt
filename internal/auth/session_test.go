package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
)

func TestSessionLoginAndRefresh(t *testing.T) {
	t.Parallel()

	var (
		pollCount      int
		refreshInvoked bool
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		case "urn:ietf:params:oauth:grant-type:device_code":
			pollCount++
			if pollCount == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"authorization_pending","error_description":"pending"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"access-1","refresh_token":"refresh-1","token_type":"Bearer","scope":"scope-a","expires_in":3600}`)
		case "refresh_token":
			refreshInvoked = true
			_, _ = io.WriteString(w, `{"access_token":"access-2","refresh_token":"refresh-2","token_type":"Bearer","scope":"scope-a","expires_in":3600}`)
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

	token.ExpiresAt = time.Now().Add(30 * time.Second)
	if err := session.store.save(token); err != nil {
		t.Fatalf("save() error = %v", err)
	}

	accessToken, err := session.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken() error = %v", err)
	}

	if accessToken != "access-2" {
		t.Fatalf("unexpected access token: %s", accessToken)
	}
	if !refreshInvoked {
		t.Fatalf("expected refresh token flow to be invoked")
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
