package appconfig

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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
	if cfg.Provider != defaultProvider {
		t.Fatalf("Provider = %q, want %q", cfg.Provider, defaultProvider)
	}
	if cfg.Auth.ClientID != defaultClientID {
		t.Fatalf("Auth.ClientID = %q, want %q", cfg.Auth.ClientID, defaultClientID)
	}
	if cfg.Auth.Tenant != defaultTenant {
		t.Fatalf("Auth.Tenant = %q, want %q", cfg.Auth.Tenant, defaultTenant)
	}
	if cfg.Auth.AuthorityBaseURL != defaultAuthorityBaseURL {
		t.Fatalf("Auth.AuthorityBaseURL = %q, want %q", cfg.Auth.AuthorityBaseURL, defaultAuthorityBaseURL)
	}
	if !reflect.DeepEqual(cfg.Auth.GraphScopes, splitScopes(defaultGraphScopes)) {
		t.Fatalf("Auth.GraphScopes = %#v, want %#v", cfg.Auth.GraphScopes, splitScopes(defaultGraphScopes))
	}
	if cfg.Auth.StateDir != wantStateDir {
		t.Fatalf("Auth.StateDir = %q, want %q", cfg.Auth.StateDir, wantStateDir)
	}
	if cfg.Mail.Client.GraphBaseURL != defaultGraphBaseURL {
		t.Fatalf("Mail.Client.GraphBaseURL = %q, want %q", cfg.Mail.Client.GraphBaseURL, defaultGraphBaseURL)
	}
	if cfg.Mail.Pipeline.OutputDir != defaultOutputDir {
		t.Fatalf("Mail.Pipeline.OutputDir = %q, want %q", cfg.Mail.Pipeline.OutputDir, defaultOutputDir)
	}
	if cfg.Mail.Pipeline.SaveOutput {
		t.Fatalf("Mail.Pipeline.SaveOutput = true, want false")
	}
	if cfg.Mail.Pipeline.BackupDir != "backup" {
		t.Fatalf("Mail.Pipeline.BackupDir = %q, want backup", cfg.Mail.Pipeline.BackupDir)
	}
	if cfg.Mail.Pipeline.AuditLogPath != DefaultAuditLogPath(wantStateDir) {
		t.Fatalf("Mail.Pipeline.AuditLogPath = %q, want %q", cfg.Mail.Pipeline.AuditLogPath, DefaultAuditLogPath(wantStateDir))
	}
	if cfg.Mail.Sync.Folder != defaultFolder {
		t.Fatalf("Mail.Sync.Folder = %q, want %q", cfg.Mail.Sync.Folder, defaultFolder)
	}
	if cfg.Mail.Sync.StateDir != wantStateDir {
		t.Fatalf("Mail.Sync.StateDir = %q, want %q", cfg.Mail.Sync.StateDir, wantStateDir)
	}
	if cfg.Mail.Sync.PollInterval != defaultPollInterval {
		t.Fatalf("Mail.Sync.PollInterval = %s, want %s", cfg.Mail.Sync.PollInterval, defaultPollInterval)
	}
	if cfg.Mail.Sync.CycleTimeout != defaultCycleTimeout {
		t.Fatalf("Mail.Sync.CycleTimeout = %s, want %s", cfg.Mail.Sync.CycleTimeout, defaultCycleTimeout)
	}
}

func TestLoadFromEnvOverrides(t *testing.T) {
	resetMimeCryptEnv(t)

	t.Setenv("MIMECRYPT_PROVIDER", "custom")
	t.Setenv("MIMECRYPT_CLIENT_ID", "client-id")
	t.Setenv("MIMECRYPT_TENANT", "tenant-id")
	t.Setenv("MIMECRYPT_AUTHORITY_BASE_URL", "https://authority.example.com")
	t.Setenv("MIMECRYPT_GRAPH_SCOPES", "scope-a scope-b")
	t.Setenv("MIMECRYPT_STATE_DIR", "/state")
	t.Setenv("MIMECRYPT_GRAPH_BASE_URL", "https://graph.example.com/v1.0")
	t.Setenv("MIMECRYPT_OUTPUT_DIR", "/output")
	t.Setenv("MIMECRYPT_SAVE_OUTPUT", "true")
	t.Setenv("MIMECRYPT_BACKUP_DIR", "/backup")
	t.Setenv("MIMECRYPT_BACKUP_KEY_ID", "backup-key")
	t.Setenv("MIMECRYPT_AUDIT_LOG_PATH", "/audit/events.jsonl")
	t.Setenv("MIMECRYPT_FOLDER", "archive")
	t.Setenv("MIMECRYPT_WRITEBACK_FOLDER", "encrypted")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	if cfg.Provider != "custom" {
		t.Fatalf("Provider = %q, want custom", cfg.Provider)
	}
	if cfg.Auth.ClientID != "client-id" {
		t.Fatalf("Auth.ClientID = %q, want client-id", cfg.Auth.ClientID)
	}
	if cfg.Auth.Tenant != "tenant-id" {
		t.Fatalf("Auth.Tenant = %q, want tenant-id", cfg.Auth.Tenant)
	}
	if cfg.Auth.AuthorityBaseURL != "https://authority.example.com" {
		t.Fatalf("Auth.AuthorityBaseURL = %q", cfg.Auth.AuthorityBaseURL)
	}
	if !reflect.DeepEqual(cfg.Auth.GraphScopes, []string{"scope-a", "scope-b"}) {
		t.Fatalf("Auth.GraphScopes = %#v", cfg.Auth.GraphScopes)
	}
	if cfg.Auth.StateDir != "/state" || cfg.Mail.Sync.StateDir != "/state" {
		t.Fatalf("unexpected state dirs: auth=%q sync=%q", cfg.Auth.StateDir, cfg.Mail.Sync.StateDir)
	}
	if cfg.Mail.Client.GraphBaseURL != "https://graph.example.com/v1.0" {
		t.Fatalf("Mail.Client.GraphBaseURL = %q", cfg.Mail.Client.GraphBaseURL)
	}
	if cfg.Mail.Pipeline.OutputDir != "/output" || !cfg.Mail.Pipeline.SaveOutput {
		t.Fatalf("unexpected pipeline output config: %+v", cfg.Mail.Pipeline)
	}
	if cfg.Mail.Pipeline.BackupDir != "/backup" || cfg.Mail.Pipeline.BackupKeyID != "backup-key" {
		t.Fatalf("unexpected backup config: %+v", cfg.Mail.Pipeline)
	}
	if cfg.Mail.Pipeline.AuditLogPath != "/audit/events.jsonl" {
		t.Fatalf("Mail.Pipeline.AuditLogPath = %q", cfg.Mail.Pipeline.AuditLogPath)
	}
	if cfg.Mail.Pipeline.WriteBackFolder != "encrypted" {
		t.Fatalf("Mail.Pipeline.WriteBackFolder = %q", cfg.Mail.Pipeline.WriteBackFolder)
	}
	if cfg.Mail.Sync.Folder != "archive" {
		t.Fatalf("Mail.Sync.Folder = %q", cfg.Mail.Sync.Folder)
	}
}

func TestLoadFromEnvRejectsInvalidSaveOutput(t *testing.T) {
	resetMimeCryptEnv(t)
	t.Setenv("MIMECRYPT_SAVE_OUTPUT", "not-a-bool")

	_, err := LoadFromEnv()
	if err == nil || !strings.Contains(err.Error(), "解析 MIMECRYPT_SAVE_OUTPUT 失败") {
		t.Fatalf("expected invalid save-output error, got %v", err)
	}
}

func TestMailConfigValidateSync(t *testing.T) {
	t.Parallel()

	base := MailConfig{
		Client: MailClientConfig{
			GraphBaseURL: "https://graph.example.com/v1.0",
		},
		Pipeline: MailPipelineConfig{
			OutputDir:    "output",
			SaveOutput:   true,
			BackupDir:    "backup",
			AuditLogPath: "audit.jsonl",
		},
		Sync: MailSyncConfig{
			Folder:       "inbox",
			StateDir:     "state",
			PollInterval: time.Minute,
			CycleTimeout: 2 * time.Minute,
		},
	}

	cases := []struct {
		name    string
		mutate  func(*MailConfig)
		wantErr string
	}{
		{
			name: "missing graph base url",
			mutate: func(cfg *MailConfig) {
				cfg.Client.GraphBaseURL = ""
			},
			wantErr: "graph base URL 不能为空",
		},
		{
			name: "missing state dir",
			mutate: func(cfg *MailConfig) {
				cfg.Sync.StateDir = ""
			},
			wantErr: "state dir 不能为空",
		},
		{
			name: "missing output dir when save enabled",
			mutate: func(cfg *MailConfig) {
				cfg.Pipeline.OutputDir = ""
			},
			wantErr: "output dir 不能为空",
		},
		{
			name: "missing backup dir",
			mutate: func(cfg *MailConfig) {
				cfg.Pipeline.BackupDir = ""
			},
			wantErr: "backup dir 不能为空",
		},
		{
			name: "missing audit log path",
			mutate: func(cfg *MailConfig) {
				cfg.Pipeline.AuditLogPath = ""
			},
			wantErr: "audit log path 不能为空",
		},
		{
			name: "missing folder",
			mutate: func(cfg *MailConfig) {
				cfg.Sync.Folder = ""
			},
			wantErr: "folder 不能为空",
		},
		{
			name: "invalid poll interval",
			mutate: func(cfg *MailConfig) {
				cfg.Sync.PollInterval = 0
			},
			wantErr: "poll interval 必须大于 0",
		},
		{
			name: "invalid cycle timeout",
			mutate: func(cfg *MailConfig) {
				cfg.Sync.CycleTimeout = 0
			},
			wantErr: "cycle timeout 必须大于 0",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := base
			tc.mutate(&cfg)

			err := cfg.ValidateSync()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateSync() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestMailConfigSyncStatePathSanitizesFolder(t *testing.T) {
	t.Parallel()

	cfg := MailConfig{
		Sync: MailSyncConfig{
			Folder:   "Archive/2026:April",
			StateDir: "/state",
		},
	}

	got := cfg.SyncStatePath()
	want := filepath.Join("/state", "sync-Archive_2026_April.json")
	if got != want {
		t.Fatalf("SyncStatePath() = %q, want %q", got, want)
	}
}

func resetMimeCryptEnv(t *testing.T) {
	t.Helper()

	keys := []string{
		"MIMECRYPT_PROVIDER",
		"MIMECRYPT_CLIENT_ID",
		"MIMECRYPT_TENANT",
		"MIMECRYPT_AUTHORITY_BASE_URL",
		"MIMECRYPT_GRAPH_SCOPES",
		"MIMECRYPT_STATE_DIR",
		"MIMECRYPT_GRAPH_BASE_URL",
		"MIMECRYPT_OUTPUT_DIR",
		"MIMECRYPT_SAVE_OUTPUT",
		"MIMECRYPT_BACKUP_DIR",
		"MIMECRYPT_BACKUP_KEY_ID",
		"MIMECRYPT_AUDIT_LOG_PATH",
		"MIMECRYPT_FOLDER",
		"MIMECRYPT_WRITEBACK_FOLDER",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}
