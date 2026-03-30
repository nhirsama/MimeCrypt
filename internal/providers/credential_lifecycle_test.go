package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/auth"
)

func TestBuildLoginRuntimeUsesDriverLoginConfigForIMAP(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	runtime, err := BuildLoginRuntime(cfg, "imap")
	if err != nil {
		t.Fatalf("BuildLoginRuntime() error = %v", err)
	}
	if runtime.ConfigName != "microsoft-oauth" {
		t.Fatalf("ConfigName = %q, want microsoft-oauth", runtime.ConfigName)
	}
	if runtime.IdentityProbe == nil {
		t.Fatalf("IdentityProbe = nil")
	}

	user, err := runtime.IdentityProbe(context.Background())
	if err != nil {
		t.Fatalf("IdentityProbe() error = %v", err)
	}
	if user.Account() != "user@example.com" {
		t.Fatalf("Account() = %q, want user@example.com", user.Account())
	}
}

func TestBuildLoginRuntimeFallsBackToConfiguredScopesWhenDriversOmitted(t *testing.T) {
	t.Parallel()

	var refreshScope string
	authServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		refreshScope = r.Form.Get("scope")
		_, _ = w.Write([]byte(`{"access_token":"access-graph","refresh_token":"refresh-next","token_type":"Bearer","scope":"scope-graph","expires_in":3600}`))
	}))
	defer authServer.Close()

	graphServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1.0/me") {
			t.Fatalf("unexpected graph request path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-graph" {
			t.Fatalf("Authorization = %q, want Bearer access-graph", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u1","mail":"user@example.com","userPrincipalName":"user@example.com"}`))
	}))
	defer graphServer.Close()

	cfg := testProviderConfig(t)
	cfg.Auth.AuthorityBaseURL = authServer.URL
	cfg.Mail.Client.GraphBaseURL = graphServer.URL + "/v1.0"

	runtime, err := BuildLoginRuntime(cfg)
	if err != nil {
		t.Fatalf("BuildLoginRuntime() error = %v", err)
	}
	session := runtime.Session.(*auth.Session)
	if err := session.StoreToken(auth.Token{
		AccessToken:  "stale-imap",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Scope:        "scope-imap",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("StoreToken() error = %v", err)
	}

	user, err := runtime.IdentityProbe(context.Background())
	if err != nil {
		t.Fatalf("IdentityProbe() error = %v", err)
	}
	if user.Account() != "user@example.com" {
		t.Fatalf("Account() = %q, want user@example.com", user.Account())
	}
	if refreshScope != "scope-graph" {
		t.Fatalf("refresh scope = %q, want scope-graph", refreshScope)
	}
}

func TestBuildRemoteRevokerUsesDriverRevokeConfigForIMAP(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1.0/me/revokeSignInSessions" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":true}`))
	}))
	defer server.Close()

	cfg := testProviderConfig(t)
	cfg.Auth.GraphScopes = nil
	cfg.Mail.Client.GraphBaseURL = server.URL + "/v1.0"

	runtime, err := BuildLoginRuntime(cfg, "imap")
	if err != nil {
		t.Fatalf("BuildLoginRuntime() error = %v", err)
	}
	revoker, effectiveCfg, err := BuildRemoteRevoker(runtime.Config, runtime.Session, "imap")
	if err != nil {
		t.Fatalf("BuildRemoteRevoker() error = %v", err)
	}
	if effectiveCfg.Mail.Client.GraphBaseURL != server.URL+"/v1.0" {
		t.Fatalf("GraphBaseURL = %q", effectiveCfg.Mail.Client.GraphBaseURL)
	}
	if revoker == nil {
		t.Fatalf("revoker = nil")
	}
}
