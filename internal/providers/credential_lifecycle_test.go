package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/interact"
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

func TestBuildLoginRuntimeRequiresConfiguredDrivers(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)

	runtime, err := BuildLoginRuntime(cfg)
	if err == nil || !strings.Contains(err.Error(), "未配置登录驱动") {
		t.Fatalf("BuildLoginRuntime() error = %v, want missing driver config error", err)
	}
	if runtime.ConfigName != "" {
		t.Fatalf("ConfigName = %q, want empty", runtime.ConfigName)
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

func TestConfigureLoginLocalConfigPromptsForDriversAndPersistsMicrosoftOverrides(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	localCfg, resolvedCfg, resolvedDrivers, err := ConfigureLoginLocalConfig(
		cfg,
		appconfig.LocalConfig{},
		strings.NewReader("imap\n\n\n\nmailbox@example.com\n"),
		io.Discard,
	)
	if err != nil {
		t.Fatalf("ConfigureLoginLocalConfig() error = %v", err)
	}
	if !reflect.DeepEqual(resolvedDrivers, []string{"imap"}) {
		t.Fatalf("resolved drivers = %#v, want [imap]", resolvedDrivers)
	}
	if !reflect.DeepEqual(localCfg.Drivers, []string{"imap"}) {
		t.Fatalf("LocalConfig.Drivers = %#v, want [imap]", localCfg.Drivers)
	}
	if localCfg.LoginConfig != "microsoft-oauth" {
		t.Fatalf("LoginConfig = %q, want microsoft-oauth", localCfg.LoginConfig)
	}
	if localCfg.IMAPUsername != "mailbox@example.com" {
		t.Fatalf("IMAPUsername = %q, want mailbox@example.com", localCfg.IMAPUsername)
	}
	if localCfg.Microsoft == nil || localCfg.Microsoft.IMAPUsername != "mailbox@example.com" {
		t.Fatalf("Microsoft = %#v, want IMAP username override", localCfg.Microsoft)
	}
	if resolvedCfg.Mail.Client.IMAPUsername != "mailbox@example.com" {
		t.Fatalf("resolved IMAP username = %q, want mailbox@example.com", resolvedCfg.Mail.Client.IMAPUsername)
	}
}

func TestConfigureLoginLocalConfigAllowsAbort(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	_, _, _, err := ConfigureLoginLocalConfig(
		cfg,
		appconfig.LocalConfig{},
		strings.NewReader("q\n"),
		io.Discard,
	)
	if !errors.Is(err, interact.ErrAbort) {
		t.Fatalf("ConfigureLoginLocalConfig() error = %v, want interact.ErrAbort", err)
	}
}
