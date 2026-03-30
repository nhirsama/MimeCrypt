package appruntime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
)

func TestBuildLoginServiceGraphIdentityProbeUsesGraphToken(t *testing.T) {
	t.Parallel()

	authServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"u1","mail":"user@example.com","userPrincipalName":"user@example.com","displayName":"User"}`)
	}))
	defer graphServer.Close()

	cfg := appconfig.Config{
		Auth: appconfig.AuthConfig{
			ClientID:         "client-id",
			Tenant:           "organizations",
			AuthorityBaseURL: authServer.URL,
			GraphScopes:      []string{"scope-graph"},
			StateDir:         t.TempDir(),
			TokenStore:       "file",
		},
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{
				GraphBaseURL: graphServer.URL + "/v1.0",
			},
		},
	}

	service, err := BuildLoginService(CredentialPlan{Config: cfg})
	if err != nil {
		t.Fatalf("BuildLoginService() error = %v", err)
	}
	session := service.Session.(*auth.Session)
	if err := session.StoreToken(auth.Token{
		AccessToken:  "stale-graph",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Scope:        "scope-imap",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("StoreToken() error = %v", err)
	}

	user, err := service.IdentityProbe(context.Background())
	if err != nil {
		t.Fatalf("IdentityProbe() error = %v", err)
	}
	if user.Account() != "user@example.com" {
		t.Fatalf("Account() = %q, want user@example.com", user.Account())
	}
}

func TestBuildLoginServiceFallsBackToIMAPUsernameIdentity(t *testing.T) {
	t.Parallel()

	service, err := BuildLoginService(CredentialPlan{
		Config: appconfig.Config{
			Auth: appconfig.AuthConfig{
				ClientID:         "client-id",
				Tenant:           "organizations",
				AuthorityBaseURL: "https://login.microsoftonline.com",
				IMAPScopes:       []string{"scope-imap"},
				StateDir:         t.TempDir(),
				TokenStore:       "file",
			},
			Mail: appconfig.MailConfig{
				Client: appconfig.MailClientConfig{
					IMAPUsername: "user@example.com",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildLoginService() error = %v", err)
	}
	if service.IdentityProbe == nil {
		t.Fatalf("IdentityProbe = nil")
	}

	user, err := service.IdentityProbe(context.Background())
	if err != nil {
		t.Fatalf("IdentityProbe() error = %v", err)
	}
	if user.Account() != "user@example.com" {
		t.Fatalf("Account() = %q, want user@example.com", user.Account())
	}
}

func TestLoginAuthConfigUsesCredentialBindingsToTrimScopes(t *testing.T) {
	t.Parallel()

	authCfg := loginAuthConfig(CredentialPlan{
		Config: appconfig.Config{
			Auth: appconfig.AuthConfig{
				GraphScopes: []string{"scope-graph"},
				EWSScopes:   []string{"scope-ews"},
				IMAPScopes:  []string{"scope-imap"},
			},
		},
		AuthDrivers: []string{"imap"},
	})

	if len(authCfg.GraphScopes) != 0 {
		t.Fatalf("GraphScopes = %#v, want empty", authCfg.GraphScopes)
	}
	if len(authCfg.EWSScopes) != 0 {
		t.Fatalf("EWSScopes = %#v, want empty", authCfg.EWSScopes)
	}
	if got := strings.Join(authCfg.IMAPScopes, " "); got != "scope-imap" {
		t.Fatalf("IMAPScopes = %#v, want [scope-imap]", authCfg.IMAPScopes)
	}
}

func TestBuildRevokeServiceForceStillClearsLocalStateWhenRemoteRevokerInitFails(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	cfg := appconfig.Config{
		Auth: appconfig.AuthConfig{
			ClientID:         "client-id",
			Tenant:           "organizations",
			AuthorityBaseURL: "https://login.microsoftonline.com",
			IMAPScopes:       []string{"scope-imap"},
			StateDir:         stateDir,
			TokenStore:       "file",
		},
	}

	if err := os.WriteFile(cfg.Auth.TokenPath(), []byte(`{"access_token":"token"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(token) error = %v", err)
	}
	if err := appconfig.SaveLocalConfig(stateDir, appconfig.LocalConfig{IMAPUsername: "user@example.com"}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	service, err := BuildRevokeService(CredentialPlan{Config: cfg}, true)
	if err != nil {
		t.Fatalf("BuildRevokeService() error = %v", err)
	}

	err = service.Run(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "初始化远端吊销器失败") {
		t.Fatalf("Run() error = %v, want remote init failure", err)
	}
	if _, statErr := os.Stat(cfg.Auth.TokenPath()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Stat(token) error = %v, want os.ErrNotExist", statErr)
	}
	if _, statErr := os.Stat(appconfig.LocalConfigPath(stateDir)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Stat(local config) error = %v, want os.ErrNotExist", statErr)
	}
}

func TestBuildRevokeServiceRejectsRemoteRevokerInitFailureWithoutForce(t *testing.T) {
	t.Parallel()

	_, err := BuildRevokeService(CredentialPlan{
		Config: appconfig.Config{
			Auth: appconfig.AuthConfig{
				ClientID:         "client-id",
				Tenant:           "organizations",
				AuthorityBaseURL: "https://login.microsoftonline.com",
				IMAPScopes:       []string{"scope-imap"},
				StateDir:         t.TempDir(),
				TokenStore:       "file",
			},
		},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "初始化远端吊销器失败") {
		t.Fatalf("BuildRevokeService() error = %v, want remote init failure", err)
	}
}

func TestBuildRevokeServiceSharedSessionSkipsRemoteRevoke(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	cfg := appconfig.Config{
		Auth: appconfig.AuthConfig{
			ClientID:         "client-id",
			Tenant:           "organizations",
			AuthorityBaseURL: "https://login.microsoftonline.com",
			IMAPScopes:       []string{"scope-imap"},
			StateDir:         stateDir,
			TokenStore:       "file",
		},
	}

	if err := os.WriteFile(cfg.Auth.TokenPath(), []byte(`{"access_token":"token"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(token) error = %v", err)
	}
	if err := appconfig.SaveLocalConfig(stateDir, appconfig.LocalConfig{IMAPUsername: "user@example.com"}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	service, err := BuildRevokeService(CredentialPlan{
		Config: cfg,
		Credential: appconfig.Credential{
			Name: "default",
			Kind: appconfig.CredentialKindSharedSession,
		},
		CredentialName: "default",
	}, false)
	if err != nil {
		t.Fatalf("BuildRevokeService() error = %v", err)
	}
	if service.RequireRemote {
		t.Fatalf("RequireRemote = true, want false for shared-session")
	}

	if err := service.Run(context.Background(), io.Discard); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if _, statErr := os.Stat(cfg.Auth.TokenPath()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Stat(token) error = %v, want os.ErrNotExist", statErr)
	}
	if _, statErr := os.Stat(appconfig.LocalConfigPath(stateDir)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Stat(local config) error = %v, want os.ErrNotExist", statErr)
	}
}
