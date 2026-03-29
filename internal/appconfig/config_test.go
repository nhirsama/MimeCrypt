package appconfig

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadFromEnvDefaults(t *testing.T) {
	resetMimeCryptEnv(t)
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	wantStateDir := filepath.Join(configHome, "mimecrypt")
	if cfg.TopologyPath != DefaultTopologyPath(wantStateDir) {
		t.Fatalf("TopologyPath = %q, want %q", cfg.TopologyPath, DefaultTopologyPath(wantStateDir))
	}
	if cfg.Auth.ClientID != defaultClientID || cfg.Auth.Tenant != defaultTenant {
		t.Fatalf("unexpected auth defaults: %+v", cfg.Auth)
	}
	if !reflect.DeepEqual(cfg.Auth.GraphScopes, splitScopes(defaultGraphScopes)) {
		t.Fatalf("GraphScopes = %#v", cfg.Auth.GraphScopes)
	}
	if cfg.Auth.StateDir != wantStateDir || cfg.Mail.Sync.StateDir != wantStateDir {
		t.Fatalf("unexpected state dirs: auth=%q sync=%q", cfg.Auth.StateDir, cfg.Mail.Sync.StateDir)
	}
	if cfg.Mail.Pipeline.OutputDir != defaultOutputDir {
		t.Fatalf("OutputDir = %q, want %q", cfg.Mail.Pipeline.OutputDir, defaultOutputDir)
	}
	if cfg.Mail.Pipeline.AuditLogPath != DefaultAuditLogPath(wantStateDir) {
		t.Fatalf("AuditLogPath = %q, want %q", cfg.Mail.Pipeline.AuditLogPath, DefaultAuditLogPath(wantStateDir))
	}
	if cfg.Mail.Sync.Folder != defaultFolder {
		t.Fatalf("Folder = %q, want %q", cfg.Mail.Sync.Folder, defaultFolder)
	}
}

func TestLoadFromEnvOverridesSupportedFields(t *testing.T) {
	resetMimeCryptEnv(t)
	t.Setenv("MIMECRYPT_CLIENT_ID", "client-id")
	t.Setenv("MIMECRYPT_TENANT", "tenant-id")
	t.Setenv("MIMECRYPT_AUTHORITY_BASE_URL", "https://authority.example.com")
	t.Setenv("MIMECRYPT_GRAPH_SCOPES", "scope-a scope-b")
	t.Setenv("MIMECRYPT_EWS_SCOPES", "scope-ews")
	t.Setenv("MIMECRYPT_IMAP_SCOPES", "scope-imap offline_access")
	t.Setenv("MIMECRYPT_STATE_DIR", "/state")
	t.Setenv("MIMECRYPT_TOPOLOGY_PATH", "/state/topology.json")
	t.Setenv("MIMECRYPT_TOKEN_STORE", "keyring")
	t.Setenv("MIMECRYPT_KEYRING_SERVICE", "mimecrypt-test")
	t.Setenv("MIMECRYPT_GRAPH_BASE_URL", "https://graph.example.com/v1.0")
	t.Setenv("MIMECRYPT_EWS_BASE_URL", "https://ews.example.com/EWS/Exchange.asmx")
	t.Setenv("MIMECRYPT_IMAP_ADDR", "imap.example.com:993")
	t.Setenv("MIMECRYPT_IMAP_USERNAME", "user@example.com")
	t.Setenv("MIMECRYPT_OUTPUT_DIR", "/output")
	t.Setenv("MIMECRYPT_WORK_DIR", "/work")
	t.Setenv("MIMECRYPT_PROTECT_SUBJECT", "true")
	t.Setenv("MIMECRYPT_BACKUP_DIR", "/backup")
	t.Setenv("MIMECRYPT_BACKUP_KEY_ID", "backup-key")
	t.Setenv("MIMECRYPT_AUDIT_LOG_PATH", "/audit/events.jsonl")
	t.Setenv("MIMECRYPT_AUDIT_STDOUT", "true")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	if cfg.TopologyPath != "/state/topology.json" {
		t.Fatalf("TopologyPath = %q", cfg.TopologyPath)
	}
	if cfg.Auth.TokenStore != "keyring" || cfg.Auth.KeyringService != "mimecrypt-test" {
		t.Fatalf("unexpected token config: %+v", cfg.Auth)
	}
	if !reflect.DeepEqual(cfg.Auth.IMAPScopes, []string{"scope-imap", "offline_access"}) {
		t.Fatalf("IMAPScopes = %#v", cfg.Auth.IMAPScopes)
	}
	if cfg.Mail.Client.IMAPUsername != "user@example.com" {
		t.Fatalf("IMAPUsername = %q", cfg.Mail.Client.IMAPUsername)
	}
	if cfg.Mail.Pipeline.WorkDir != "/work" || !cfg.Mail.Pipeline.ProtectSubject {
		t.Fatalf("unexpected pipeline config: %+v", cfg.Mail.Pipeline)
	}
	if cfg.Mail.Pipeline.AuditLogPath != "/audit/events.jsonl" || !cfg.Mail.Pipeline.AuditStdout {
		t.Fatalf("unexpected audit config: %+v", cfg.Mail.Pipeline)
	}
}

func TestLoadFromEnvRejectsInvalidBooleans(t *testing.T) {
	for _, tc := range []struct {
		key     string
		wantErr string
	}{
		{key: "MIMECRYPT_PROTECT_SUBJECT", wantErr: "解析 MIMECRYPT_PROTECT_SUBJECT 失败"},
		{key: "MIMECRYPT_AUDIT_STDOUT", wantErr: "解析 MIMECRYPT_AUDIT_STDOUT 失败"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			resetMimeCryptEnv(t)
			t.Setenv(tc.key, "not-a-bool")
			_, err := LoadFromEnv()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("LoadFromEnv() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadFromEnvUsesSavedIMAPUsernameWhenEnvMissing(t *testing.T) {
	resetMimeCryptEnv(t)

	stateDir := t.TempDir()
	t.Setenv("MIMECRYPT_STATE_DIR", stateDir)
	if err := SaveLocalConfig(stateDir, LocalConfig{IMAPUsername: "saved@example.com"}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.Mail.Client.IMAPUsername != "saved@example.com" {
		t.Fatalf("Mail.Client.IMAPUsername = %q, want saved@example.com", cfg.Mail.Client.IMAPUsername)
	}
}

func TestConfigWithStateDirRebasesDefaultAuditLogPath(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Auth: AuthConfig{StateDir: "/old"},
		Mail: MailConfig{
			Pipeline: MailPipelineConfig{
				AuditLogPath: DefaultAuditLogPath("/old"),
			},
			Sync: MailSyncConfig{StateDir: "/old"},
		},
	}

	got := cfg.WithStateDir("/next-state")
	if got.Auth.StateDir != "/next-state" || got.Mail.Sync.StateDir != "/next-state" {
		t.Fatalf("unexpected state dirs: auth=%q sync=%q", got.Auth.StateDir, got.Mail.Sync.StateDir)
	}
	if got.Mail.Pipeline.AuditLogPath != DefaultAuditLogPath("/next-state") {
		t.Fatalf("AuditLogPath = %q", got.Mail.Pipeline.AuditLogPath)
	}
}

func TestConfigWithCredentialAppliesCredentialScopedStateAndStoredIMAPUsername(t *testing.T) {
	t.Parallel()

	baseStateDir := t.TempDir()
	credentialStateDir := DefaultCredentialStateDir(baseStateDir, "archive")
	if err := SaveLocalConfig(credentialStateDir, LocalConfig{IMAPUsername: "archive@example.com"}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	cfg := Config{
		Auth: AuthConfig{
			ClientID:   "client-id",
			StateDir:   baseStateDir,
			TokenStore: "file",
		},
		Mail: MailConfig{
			Client: MailClientConfig{
				IMAPUsername: "base@example.com",
			},
			Pipeline: MailPipelineConfig{
				AuditLogPath: DefaultAuditLogPath(baseStateDir),
			},
			Sync: MailSyncConfig{StateDir: baseStateDir},
		},
	}

	got := cfg.WithCredential("archive", Credential{
		Name:       "archive",
		Kind:       "oauth",
		TokenStore: "keyring",
	})

	if got.Auth.StateDir != credentialStateDir || got.Mail.Sync.StateDir != credentialStateDir {
		t.Fatalf("unexpected scoped state dir: auth=%q sync=%q", got.Auth.StateDir, got.Mail.Sync.StateDir)
	}
	if got.Auth.TokenStore != "keyring" {
		t.Fatalf("TokenStore = %q, want keyring", got.Auth.TokenStore)
	}
	if got.Mail.Client.IMAPUsername != "archive@example.com" {
		t.Fatalf("IMAPUsername = %q, want archive@example.com", got.Mail.Client.IMAPUsername)
	}
}

func TestMailConfigValidatePipelineBase(t *testing.T) {
	t.Parallel()

	cfg := MailConfig{
		Pipeline: MailPipelineConfig{
			BackupDir:    "backup",
			AuditLogPath: "audit.jsonl",
		},
		Sync: MailSyncConfig{
			StateDir: "/state",
		},
	}
	if err := cfg.ValidatePipelineBase(); err != nil {
		t.Fatalf("ValidatePipelineBase() error = %v", err)
	}

	cfg.Pipeline.BackupDir = ""
	if err := cfg.ValidatePipelineBase(); err == nil || !strings.Contains(err.Error(), "backup dir") {
		t.Fatalf("ValidatePipelineBase() error = %v, want backup dir error", err)
	}
}

func TestStateLayoutHelpersUseFullScope(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Auth: AuthConfig{StateDir: "/state"},
		Mail: MailConfig{
			Sync: MailSyncConfig{StateDir: "/state"},
		},
	}

	if got, want := cfg.Mail.FlowProducerStatePathFor("default", "imap", "Archive/2026"), filepath.Join("/state", "flow-sync-default-imap-Archive_2026.json"); got != want {
		t.Fatalf("FlowProducerStatePathFor() = %q, want %q", got, want)
	}
	if got, want := cfg.Mail.FlowStateDirFor("default", "default", "imap", "Archive/2026"), filepath.Join("/state", "flow-state", "default-default-imap-Archive_2026"); got != want {
		t.Fatalf("FlowStateDirFor() = %q, want %q", got, want)
	}
	if got, want := cfg.RunLockPathFor("default", "imap", "Archive/2026"), filepath.Join("/state", "run-default-imap-Archive_2026.lock"); got != want {
		t.Fatalf("RunLockPathFor() = %q, want %q", got, want)
	}
}

func resetMimeCryptEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"MIMECRYPT_CLIENT_ID",
		"MIMECRYPT_TENANT",
		"MIMECRYPT_AUTHORITY_BASE_URL",
		"MIMECRYPT_GRAPH_SCOPES",
		"MIMECRYPT_EWS_SCOPES",
		"MIMECRYPT_IMAP_SCOPES",
		"MIMECRYPT_STATE_DIR",
		"MIMECRYPT_TOPOLOGY_PATH",
		"MIMECRYPT_TOKEN_STORE",
		"MIMECRYPT_KEYRING_SERVICE",
		"MIMECRYPT_GRAPH_BASE_URL",
		"MIMECRYPT_EWS_BASE_URL",
		"MIMECRYPT_IMAP_ADDR",
		"MIMECRYPT_IMAP_USERNAME",
		"MIMECRYPT_OUTPUT_DIR",
		"MIMECRYPT_WORK_DIR",
		"MIMECRYPT_PROTECT_SUBJECT",
		"MIMECRYPT_BACKUP_DIR",
		"MIMECRYPT_BACKUP_KEY_ID",
		"MIMECRYPT_AUDIT_LOG_PATH",
		"MIMECRYPT_AUDIT_STDOUT",
	} {
		t.Setenv(key, "")
	}
}
