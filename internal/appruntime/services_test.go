package appruntime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
)

func TestBuildHealthServiceMixedProviderWriteBackProbeUsesGraphToken(t *testing.T) {
	t.Parallel()

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/organizations/oauth2/v2.0/token" {
			t.Fatalf("unexpected auth request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.Form.Get("scope"); got != "scope-graph" {
			t.Fatalf("refresh scope = %q, want scope-graph", got)
		}
		_, _ = io.WriteString(w, `{"access_token":"access-graph","refresh_token":"refresh-next","token_type":"Bearer","scope":"scope-graph","expires_in":3600}`)
	}))
	defer authServer.Close()

	graphServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1.0/me") {
			t.Fatalf("unexpected graph request path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-graph" {
			t.Fatalf("Authorization = %q, want Bearer access-graph", got)
		}
		_, _ = io.WriteString(w, `{"id":"u1","mail":"user@example.com","userPrincipalName":"user@example.com"}`)
	}))
	defer graphServer.Close()

	cfg := appconfig.Config{
		Provider: "imap",
		Auth: appconfig.AuthConfig{
			ClientID:         "client-id",
			Tenant:           "organizations",
			AuthorityBaseURL: authServer.URL,
			GraphScopes:      []string{"scope-graph"},
			IMAPScopes:       []string{"scope-imap"},
			StateDir:         t.TempDir(),
			TokenStore:       "file",
		},
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{
				GraphBaseURL: graphServer.URL + "/v1.0",
				IMAPAddr:     "imap.example.com:993",
				IMAPUsername: "user@example.com",
			},
			Pipeline: appconfig.MailPipelineConfig{
				WriteBackProvider: "graph",
			},
			Sync: appconfig.MailSyncConfig{
				Folder: "INBOX",
			},
		},
	}

	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := session.StoreToken(auth.Token{
		AccessToken:  "stale-imap",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Scope:        "scope-imap",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("StoreToken() error = %v", err)
	}

	service, err := BuildHealthService(cfg)
	if err != nil {
		t.Fatalf("BuildHealthService() error = %v", err)
	}
	if service.WriteBack == nil {
		t.Fatalf("WriteBack = nil")
	}

	detail, err := service.WriteBack.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if detail != "user@example.com" {
		t.Fatalf("detail = %q, want user@example.com", detail)
	}
}
