package providers

import (
	"bufio"
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

func TestBuildLoginRuntimeUsesRuntimeRegistryWithIMAPHint(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	runtime, err := BuildLoginRuntime(cfg, "imap")
	if err != nil {
		t.Fatalf("BuildLoginRuntime() error = %v", err)
	}
	if runtime.RuntimeName != "oauth-device" {
		t.Fatalf("RuntimeName = %q, want oauth-device", runtime.RuntimeName)
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

func TestBuildCredentialRuntimeCarriesNameAndKind(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	runtime, err := BuildCredentialRuntime("office-auth", appconfig.CredentialKindOAuth, oauthDeviceRuntimeName, cfg, "imap")
	if err != nil {
		t.Fatalf("BuildCredentialRuntime() error = %v", err)
	}
	if runtime.Name != "office-auth" {
		t.Fatalf("Name = %q, want office-auth", runtime.Name)
	}
	if runtime.Kind != appconfig.CredentialKindOAuth {
		t.Fatalf("Kind = %q, want oauth", runtime.Kind)
	}
}

func TestBuildCredentialRuntimeUsesConfiguredRuntimeWithoutDriverHints(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	runtime, err := BuildCredentialRuntime("office-auth", appconfig.CredentialKindOAuth, oauthDeviceRuntimeName, cfg)
	if err != nil {
		t.Fatalf("BuildCredentialRuntime() error = %v", err)
	}
	if runtime.RuntimeName != oauthDeviceRuntimeName {
		t.Fatalf("RuntimeName = %q, want %q", runtime.RuntimeName, oauthDeviceRuntimeName)
	}
	if len(runtime.Drivers) != 0 {
		t.Fatalf("Drivers = %#v, want empty without hints", runtime.Drivers)
	}
}

func TestBuildCredentialRuntimeRequiresConfiguredRuntime(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	runtime, err := BuildCredentialRuntime("office-auth", appconfig.CredentialKindOAuth, "", cfg)
	if err == nil || !strings.Contains(err.Error(), "未配置运行时驱动") {
		t.Fatalf("BuildCredentialRuntime() error = %v, want missing runtime config", err)
	}
	if runtime.RuntimeName != "" {
		t.Fatalf("RuntimeName = %q, want empty", runtime.RuntimeName)
	}
}

func TestBuildRemoteRevokerUsesRuntimeRegistryWithIMAPHint(t *testing.T) {
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
	revoker, effectiveCfg, err := BuildRemoteRevoker(appconfig.CredentialKindOAuth, runtime.RuntimeName, runtime.Config, runtime.Session, "imap")
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

func TestConfigureLoginLocalConfigUsesExplicitAuthHintsAndPersistsMicrosoftOverrides(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	var out strings.Builder
	localCfg, resolvedCfg, resolvedDrivers, err := ConfigureLoginLocalConfig(
		appconfig.CredentialKindOAuth,
		cfg,
		appconfig.LocalConfig{},
		strings.NewReader("\n\n\nmailbox@example.com\n"),
		&out,
		"imap",
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
	if localCfg.LoginConfig != "oauth-device" {
		t.Fatalf("LoginConfig = %q, want oauth-device", localCfg.LoginConfig)
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
	if strings.Contains(out.String(), "请选择 credential 需要支持的邮件驱动") {
		t.Fatalf("unexpected driver selection prompt: %q", out.String())
	}
	if strings.Contains(out.String(), "认证提示") || strings.Contains(out.String(), "提示:") {
		t.Fatalf("unexpected auth hint selection prompt: %q", out.String())
	}
	if strings.Contains(out.String(), "邮件驱动") || strings.Contains(out.String(), "驱动:") {
		t.Fatalf("unexpected driver runtime wording: %q", out.String())
	}
}

func TestConfigureLoginLocalConfigPromptsForAuthHintsInsteadOfMailDrivers(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	var out strings.Builder
	localCfg, resolvedCfg, resolvedDrivers, err := ConfigureLoginLocalConfig(
		appconfig.CredentialKindOAuth,
		cfg,
		appconfig.LocalConfig{},
		strings.NewReader("imap\n\n\n\nmailbox@example.com\n"),
		&out,
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
	if localCfg.LoginConfig != oauthDeviceRuntimeName {
		t.Fatalf("LoginConfig = %q, want %q", localCfg.LoginConfig, oauthDeviceRuntimeName)
	}
	if localCfg.IMAPUsername != "mailbox@example.com" {
		t.Fatalf("LocalConfig.IMAPUsername = %q, want mailbox@example.com", localCfg.IMAPUsername)
	}
	if resolvedCfg.Mail.Client.IMAPUsername != "mailbox@example.com" {
		t.Fatalf("resolved IMAP username = %q, want mailbox@example.com", resolvedCfg.Mail.Client.IMAPUsername)
	}
	if !strings.Contains(out.String(), "认证提示") || !strings.Contains(out.String(), "提示:") {
		t.Fatalf("missing auth hint prompt: %q", out.String())
	}
	if strings.Contains(out.String(), "邮件驱动") || strings.Contains(out.String(), "驱动:") {
		t.Fatalf("unexpected driver runtime wording: %q", out.String())
	}
}

func TestConfigureLoginLocalConfigAllowsAbort(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	_, _, _, err := ConfigureLoginLocalConfig(
		appconfig.CredentialKindOAuth,
		cfg,
		appconfig.LocalConfig{},
		strings.NewReader("q\n"),
		io.Discard,
	)
	if !errors.Is(err, interact.ErrAbort) {
		t.Fatalf("ConfigureLoginLocalConfig() error = %v, want interact.ErrAbort", err)
	}
}

func TestPromptConfigValueUsesEffectiveValueOnBlankInput(t *testing.T) {
	t.Parallel()

	got, err := promptConfigValue(bufio.NewReader(strings.NewReader("\n")), io.Discard, "Client ID", "base-client", "")
	if err != nil {
		t.Fatalf("promptConfigValue() error = %v", err)
	}
	if got != "base-client" {
		t.Fatalf("blank prompt result = %q, want base-client", got)
	}

	got, err = promptConfigValue(bufio.NewReader(strings.NewReader("\n")), io.Discard, "Client ID", "base-client", "stored-client")
	if err != nil {
		t.Fatalf("promptConfigValue() error = %v", err)
	}
	if got != "stored-client" {
		t.Fatalf("blank prompt result = %q, want stored-client", got)
	}
}

func TestConfigureLoginLocalConfigClearsStoredIMAPOverrideWhenDriverDropsIMAP(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	localCfg, resolvedCfg, resolvedDrivers, err := ConfigureLoginLocalConfig(
		appconfig.CredentialKindOAuth,
		cfg,
		appconfig.LocalConfig{
			Drivers:      []string{"imap"},
			LoginConfig:  oauthDeviceRuntimeName,
			IMAPUsername: "stored@example.com",
			Microsoft: &appconfig.MicrosoftLocalConfig{
				ClientID:         "client-id",
				Tenant:           "organizations",
				AuthorityBaseURL: "https://login.microsoftonline.com",
				IMAPUsername:     "stored@example.com",
			},
		},
		strings.NewReader("\n\n\n"),
		io.Discard,
		"graph",
	)
	if err != nil {
		t.Fatalf("ConfigureLoginLocalConfig() error = %v", err)
	}
	if !reflect.DeepEqual(resolvedDrivers, []string{"graph"}) {
		t.Fatalf("resolved drivers = %#v, want [graph]", resolvedDrivers)
	}
	if localCfg.IMAPUsername != "" {
		t.Fatalf("LocalConfig.IMAPUsername = %q, want empty", localCfg.IMAPUsername)
	}
	if localCfg.Microsoft == nil || localCfg.Microsoft.IMAPUsername != "" {
		t.Fatalf("Microsoft IMAP username = %#v, want empty override", localCfg.Microsoft)
	}
	if resolvedCfg.Mail.Client.IMAPUsername != cfg.Mail.Client.IMAPUsername {
		t.Fatalf("resolved IMAP username = %q, want base config %q", resolvedCfg.Mail.Client.IMAPUsername, cfg.Mail.Client.IMAPUsername)
	}
	if len(resolvedCfg.Auth.IMAPScopes) != 0 {
		t.Fatalf("IMAPScopes = %#v, want empty after graph-only runtime", resolvedCfg.Auth.IMAPScopes)
	}
}

func TestConfigureLoginLocalConfigNormalizesLegacyStoredRuntimeName(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	localCfg, resolvedCfg, resolvedDrivers, err := ConfigureLoginLocalConfig(
		appconfig.CredentialKindOAuth,
		cfg,
		appconfig.LocalConfig{
			Drivers:     []string{"graph"},
			LoginConfig: legacyMicrosoftOAuthRuntime,
			Microsoft: &appconfig.MicrosoftLocalConfig{
				ClientID:         "stored-client",
				Tenant:           "stored-tenant",
				AuthorityBaseURL: "https://login.example.com",
			},
		},
		strings.NewReader("\n\n\n\n"),
		io.Discard,
	)
	if err != nil {
		t.Fatalf("ConfigureLoginLocalConfig() error = %v", err)
	}
	if localCfg.LoginConfig != oauthDeviceRuntimeName {
		t.Fatalf("LoginConfig = %q, want %q", localCfg.LoginConfig, oauthDeviceRuntimeName)
	}
	if !reflect.DeepEqual(resolvedDrivers, []string{"graph"}) {
		t.Fatalf("resolved drivers = %#v, want [graph]", resolvedDrivers)
	}
	if resolvedCfg.Auth.ClientID != "stored-client" {
		t.Fatalf("resolved ClientID = %q, want stored-client", resolvedCfg.Auth.ClientID)
	}
}

func TestBuildRemoteRevokerPreservesEffectiveConfigOnInitError(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)

	revoker, effectiveCfg, err := BuildRemoteRevoker(appconfig.CredentialKindOAuth, oauthDeviceRuntimeName, cfg, nil)
	if effectiveCfg.Auth.StateDir != cfg.Auth.StateDir {
		t.Fatalf("effective Auth.StateDir = %q, want %q", effectiveCfg.Auth.StateDir, cfg.Auth.StateDir)
	}
	if err == nil || !strings.Contains(err.Error(), "token source 不能为空") {
		t.Fatalf("BuildRemoteRevoker() error = %v, want missing token source", err)
	}
	if revoker != nil {
		t.Fatalf("revoker = %#v, want nil", revoker)
	}
}

func TestBuildCredentialRuntimeAcceptsLegacyStoredRuntimeName(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	runtime, err := BuildCredentialRuntime("office-auth", appconfig.CredentialKindOAuth, legacyMicrosoftOAuthRuntime, cfg)
	if err != nil {
		t.Fatalf("BuildCredentialRuntime() error = %v", err)
	}
	if runtime.RuntimeName != oauthDeviceRuntimeName {
		t.Fatalf("RuntimeName = %q, want %s", runtime.RuntimeName, oauthDeviceRuntimeName)
	}
}

func TestAvailableCredentialAuthHintNamesUsesIndependentRegistry(t *testing.T) {
	t.Parallel()

	got := availableCredentialAuthHintNames()
	want := []string{"ews", "graph", "imap"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("availableCredentialAuthHintNames() = %#v, want %#v", got, want)
	}
}

func TestLookupCredentialRuntimeRegistrationAcceptsLegacyAlias(t *testing.T) {
	t.Parallel()

	registration, ok := lookupCredentialRuntimeRegistration(legacyMicrosoftOAuthRuntime)
	if !ok {
		t.Fatalf("lookupCredentialRuntimeRegistration() ok = false, want true")
	}
	if registration.Name != oauthDeviceRuntimeName {
		t.Fatalf("registration.Name = %q, want %q", registration.Name, oauthDeviceRuntimeName)
	}
}
