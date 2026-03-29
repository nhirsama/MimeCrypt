package appconfig

import (
	"os"
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
	if cfg.TopologyPath != "" {
		t.Fatalf("TopologyPath = %q, want empty", cfg.TopologyPath)
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
	if !reflect.DeepEqual(cfg.Auth.EWSScopes, splitScopes(defaultEWSScopes)) {
		t.Fatalf("Auth.EWSScopes = %#v, want %#v", cfg.Auth.EWSScopes, splitScopes(defaultEWSScopes))
	}
	if !reflect.DeepEqual(cfg.Auth.IMAPScopes, splitScopes(defaultIMAPScopes)) {
		t.Fatalf("Auth.IMAPScopes = %#v, want %#v", cfg.Auth.IMAPScopes, splitScopes(defaultIMAPScopes))
	}
	if cfg.Auth.StateDir != wantStateDir {
		t.Fatalf("Auth.StateDir = %q, want %q", cfg.Auth.StateDir, wantStateDir)
	}
	if cfg.Auth.TokenStore != defaultTokenStore {
		t.Fatalf("Auth.TokenStore = %q, want %q", cfg.Auth.TokenStore, defaultTokenStore)
	}
	if cfg.Auth.KeyringService != defaultKeyringService {
		t.Fatalf("Auth.KeyringService = %q, want %q", cfg.Auth.KeyringService, defaultKeyringService)
	}
	if cfg.Mail.Client.GraphBaseURL != defaultGraphBaseURL {
		t.Fatalf("Mail.Client.GraphBaseURL = %q, want %q", cfg.Mail.Client.GraphBaseURL, defaultGraphBaseURL)
	}
	if cfg.Mail.Client.EWSBaseURL != defaultEWSBaseURL {
		t.Fatalf("Mail.Client.EWSBaseURL = %q, want %q", cfg.Mail.Client.EWSBaseURL, defaultEWSBaseURL)
	}
	if cfg.Mail.Client.IMAPAddr != defaultIMAPAddr {
		t.Fatalf("Mail.Client.IMAPAddr = %q, want %q", cfg.Mail.Client.IMAPAddr, defaultIMAPAddr)
	}
	if cfg.Mail.Pipeline.OutputDir != defaultOutputDir {
		t.Fatalf("Mail.Pipeline.OutputDir = %q, want %q", cfg.Mail.Pipeline.OutputDir, defaultOutputDir)
	}
	if cfg.Mail.Pipeline.SaveOutput {
		t.Fatalf("Mail.Pipeline.SaveOutput = true, want false")
	}
	if cfg.Mail.Pipeline.WorkDir != "" {
		t.Fatalf("Mail.Pipeline.WorkDir = %q, want empty", cfg.Mail.Pipeline.WorkDir)
	}
	if cfg.Mail.Pipeline.BackupDir != "backup" {
		t.Fatalf("Mail.Pipeline.BackupDir = %q, want backup", cfg.Mail.Pipeline.BackupDir)
	}
	if cfg.Mail.Pipeline.AuditLogPath != DefaultAuditLogPath(wantStateDir) {
		t.Fatalf("Mail.Pipeline.AuditLogPath = %q, want %q", cfg.Mail.Pipeline.AuditLogPath, DefaultAuditLogPath(wantStateDir))
	}
	if cfg.Mail.Pipeline.AuditStdout {
		t.Fatalf("Mail.Pipeline.AuditStdout = true, want false")
	}
	if cfg.Mail.Pipeline.WriteBackProvider != defaultWriteBackProvider {
		t.Fatalf("Mail.Pipeline.WriteBackProvider = %q, want %q", cfg.Mail.Pipeline.WriteBackProvider, defaultWriteBackProvider)
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
	if cfg.RunLockPath() != filepath.Join(wantStateDir, "run-imap-INBOX.lock") {
		t.Fatalf("RunLockPath() = %q", cfg.RunLockPath())
	}
}

func TestLoadFromEnvOverrides(t *testing.T) {
	resetMimeCryptEnv(t)

	t.Setenv("MIMECRYPT_PROVIDER", "custom")
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
	t.Setenv("MIMECRYPT_SAVE_OUTPUT", "true")
	t.Setenv("MIMECRYPT_WORK_DIR", "/work")
	t.Setenv("MIMECRYPT_PROTECT_SUBJECT", "true")
	t.Setenv("MIMECRYPT_BACKUP_DIR", "/backup")
	t.Setenv("MIMECRYPT_BACKUP_KEY_ID", "backup-key")
	t.Setenv("MIMECRYPT_AUDIT_LOG_PATH", "/audit/events.jsonl")
	t.Setenv("MIMECRYPT_AUDIT_STDOUT", "true")
	t.Setenv("MIMECRYPT_WRITEBACK_PROVIDER", "graph")
	t.Setenv("MIMECRYPT_FOLDER", "archive")
	t.Setenv("MIMECRYPT_WRITEBACK_FOLDER", "encrypted")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	if cfg.Provider != "custom" {
		t.Fatalf("Provider = %q, want custom", cfg.Provider)
	}
	if cfg.TopologyPath != "/state/topology.json" {
		t.Fatalf("TopologyPath = %q, want /state/topology.json", cfg.TopologyPath)
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
	if !reflect.DeepEqual(cfg.Auth.EWSScopes, []string{"scope-ews"}) {
		t.Fatalf("Auth.EWSScopes = %#v", cfg.Auth.EWSScopes)
	}
	if !reflect.DeepEqual(cfg.Auth.IMAPScopes, []string{"scope-imap", "offline_access"}) {
		t.Fatalf("Auth.IMAPScopes = %#v", cfg.Auth.IMAPScopes)
	}
	if cfg.Auth.StateDir != "/state" || cfg.Mail.Sync.StateDir != "/state" {
		t.Fatalf("unexpected state dirs: auth=%q sync=%q", cfg.Auth.StateDir, cfg.Mail.Sync.StateDir)
	}
	if cfg.Auth.TokenStore != "keyring" {
		t.Fatalf("Auth.TokenStore = %q, want keyring", cfg.Auth.TokenStore)
	}
	if cfg.Auth.KeyringService != "mimecrypt-test" {
		t.Fatalf("Auth.KeyringService = %q, want mimecrypt-test", cfg.Auth.KeyringService)
	}
	if cfg.Mail.Client.GraphBaseURL != "https://graph.example.com/v1.0" {
		t.Fatalf("Mail.Client.GraphBaseURL = %q", cfg.Mail.Client.GraphBaseURL)
	}
	if cfg.Mail.Client.EWSBaseURL != "https://ews.example.com/EWS/Exchange.asmx" {
		t.Fatalf("Mail.Client.EWSBaseURL = %q", cfg.Mail.Client.EWSBaseURL)
	}
	if cfg.Mail.Client.IMAPAddr != "imap.example.com:993" || cfg.Mail.Client.IMAPUsername != "user@example.com" {
		t.Fatalf("unexpected IMAP client config: %+v", cfg.Mail.Client)
	}
	if cfg.Mail.Pipeline.OutputDir != "/output" || !cfg.Mail.Pipeline.SaveOutput {
		t.Fatalf("unexpected pipeline output config: %+v", cfg.Mail.Pipeline)
	}
	if cfg.Mail.Pipeline.WorkDir != "/work" {
		t.Fatalf("Mail.Pipeline.WorkDir = %q, want /work", cfg.Mail.Pipeline.WorkDir)
	}
	if !cfg.Mail.Pipeline.ProtectSubject {
		t.Fatalf("Mail.Pipeline.ProtectSubject = false, want true")
	}
	if cfg.Mail.Pipeline.BackupDir != "/backup" || cfg.Mail.Pipeline.BackupKeyID != "backup-key" {
		t.Fatalf("unexpected backup config: %+v", cfg.Mail.Pipeline)
	}
	if cfg.Mail.Pipeline.AuditLogPath != "/audit/events.jsonl" {
		t.Fatalf("Mail.Pipeline.AuditLogPath = %q", cfg.Mail.Pipeline.AuditLogPath)
	}
	if !cfg.Mail.Pipeline.AuditStdout {
		t.Fatalf("Mail.Pipeline.AuditStdout = false, want true")
	}
	if cfg.Mail.Pipeline.WriteBackFolder != "encrypted" {
		t.Fatalf("Mail.Pipeline.WriteBackFolder = %q", cfg.Mail.Pipeline.WriteBackFolder)
	}
	if cfg.Mail.Pipeline.WriteBackProvider != "graph" {
		t.Fatalf("Mail.Pipeline.WriteBackProvider = %q", cfg.Mail.Pipeline.WriteBackProvider)
	}
	if cfg.Mail.Sync.Folder != "archive" {
		t.Fatalf("Mail.Sync.Folder = %q", cfg.Mail.Sync.Folder)
	}
	if cfg.RunLockPath() != filepath.Join("/state", "run-custom-archive.lock") {
		t.Fatalf("RunLockPath() = %q", cfg.RunLockPath())
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

func TestLoadFromEnvRejectsInvalidProtectSubject(t *testing.T) {
	resetMimeCryptEnv(t)
	t.Setenv("MIMECRYPT_PROTECT_SUBJECT", "not-a-bool")

	_, err := LoadFromEnv()
	if err == nil || !strings.Contains(err.Error(), "解析 MIMECRYPT_PROTECT_SUBJECT 失败") {
		t.Fatalf("expected invalid protect-subject error, got %v", err)
	}
}

func TestLoadFromEnvRejectsInvalidAuditStdout(t *testing.T) {
	resetMimeCryptEnv(t)
	t.Setenv("MIMECRYPT_AUDIT_STDOUT", "not-a-bool")

	_, err := LoadFromEnv()
	if err == nil || !strings.Contains(err.Error(), "解析 MIMECRYPT_AUDIT_STDOUT 失败") {
		t.Fatalf("expected invalid audit-stdout error, got %v", err)
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

func TestLoadFromEnvEnvOverridesSavedIMAPUsername(t *testing.T) {
	resetMimeCryptEnv(t)

	stateDir := t.TempDir()
	t.Setenv("MIMECRYPT_STATE_DIR", stateDir)
	t.Setenv("MIMECRYPT_IMAP_USERNAME", "env@example.com")
	if err := SaveLocalConfig(stateDir, LocalConfig{IMAPUsername: "saved@example.com"}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.Mail.Client.IMAPUsername != "env@example.com" {
		t.Fatalf("Mail.Client.IMAPUsername = %q, want env@example.com", cfg.Mail.Client.IMAPUsername)
	}
}

func TestSaveAndLoadLocalConfig(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	if err := SaveLocalConfig(stateDir, LocalConfig{IMAPUsername: " user@example.com "}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	cfg, err := LoadLocalConfig(stateDir)
	if err != nil {
		t.Fatalf("LoadLocalConfig() error = %v", err)
	}
	if cfg.IMAPUsername != "user@example.com" {
		t.Fatalf("IMAPUsername = %q, want user@example.com", cfg.IMAPUsername)
	}

	info, err := os.Stat(LocalConfigPath(stateDir))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestResolveStoredIMAPUsernameUsesSavedValueWhenFallbackEmpty(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	if err := SaveLocalConfig(stateDir, LocalConfig{IMAPUsername: "saved@example.com"}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	if got, want := ResolveStoredIMAPUsername(stateDir, ""), "saved@example.com"; got != want {
		t.Fatalf("ResolveStoredIMAPUsername() = %q, want %q", got, want)
	}
}

func TestConfigWithCredentialUsesDerivedStateDirAndStoredIMAPUsername(t *testing.T) {
	t.Parallel()

	baseStateDir := t.TempDir()
	credentialStateDir := DefaultCredentialStateDir(baseStateDir, "archive")
	if err := SaveLocalConfig(credentialStateDir, LocalConfig{IMAPUsername: "archive@example.com"}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	cfg := Config{
		Auth: AuthConfig{
			ClientID:         "base-client",
			Tenant:           "base-tenant",
			AuthorityBaseURL: "https://authority.example.com",
			GraphScopes:      []string{"graph.read"},
			EWSScopes:        []string{"ews.read"},
			IMAPScopes:       []string{"imap.read"},
			StateDir:         baseStateDir,
			TokenStore:       "file",
			KeyringService:   "mimecrypt",
		},
		Mail: MailConfig{
			Client: MailClientConfig{
				IMAPUsername: "",
			},
		},
	}

	got := cfg.WithCredential("archive", Credential{
		Name:           "archive",
		Kind:           "oauth",
		ClientID:       "credential-client",
		TokenStore:     "keyring",
		KeyringService: "archive-keyring",
		GraphScopes:    []string{"graph.archive"},
	})

	if got.Auth.StateDir != credentialStateDir {
		t.Fatalf("Auth.StateDir = %q, want %q", got.Auth.StateDir, credentialStateDir)
	}
	if got.Mail.Sync.StateDir != credentialStateDir {
		t.Fatalf("Mail.Sync.StateDir = %q, want %q", got.Mail.Sync.StateDir, credentialStateDir)
	}
	if got.Auth.ClientID != "credential-client" {
		t.Fatalf("Auth.ClientID = %q, want credential-client", got.Auth.ClientID)
	}
	if got.Auth.TokenStore != "keyring" || got.Auth.KeyringService != "archive-keyring" {
		t.Fatalf("unexpected token store config: %+v", got.Auth)
	}
	if !reflect.DeepEqual(got.Auth.GraphScopes, []string{"graph.archive"}) {
		t.Fatalf("Auth.GraphScopes = %#v", got.Auth.GraphScopes)
	}
	if got.Mail.Client.IMAPUsername != "archive@example.com" {
		t.Fatalf("IMAPUsername = %q, want archive@example.com", got.Mail.Client.IMAPUsername)
	}
}

func TestConfigWithCredentialUpdatesDefaultAuditLogPathWithStateDir(t *testing.T) {
	t.Parallel()

	baseStateDir := t.TempDir()
	credentialStateDir := DefaultCredentialStateDir(baseStateDir, "archive")

	cfg := Config{
		Auth: AuthConfig{
			StateDir: baseStateDir,
		},
		Mail: MailConfig{
			Pipeline: MailPipelineConfig{
				AuditLogPath: DefaultAuditLogPath(baseStateDir),
			},
			Sync: MailSyncConfig{
				StateDir: baseStateDir,
			},
		},
	}

	got := cfg.WithCredential("archive", Credential{
		Name: "archive",
		Kind: "oauth",
	})

	if got.Mail.Pipeline.AuditLogPath != DefaultAuditLogPath(credentialStateDir) {
		t.Fatalf("AuditLogPath = %q, want %q", got.Mail.Pipeline.AuditLogPath, DefaultAuditLogPath(credentialStateDir))
	}
}

func TestConfigWithStateDirKeepsCustomAuditLogPath(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Auth: AuthConfig{
			StateDir: "/state",
		},
		Mail: MailConfig{
			Pipeline: MailPipelineConfig{
				AuditLogPath: "/logs/custom-audit.jsonl",
			},
			Sync: MailSyncConfig{
				StateDir: "/state",
			},
		},
	}

	got := cfg.WithStateDir("/next-state")

	if got.Auth.StateDir != "/next-state" || got.Mail.Sync.StateDir != "/next-state" {
		t.Fatalf("unexpected state dirs: auth=%q sync=%q", got.Auth.StateDir, got.Mail.Sync.StateDir)
	}
	if got.Mail.Pipeline.AuditLogPath != "/logs/custom-audit.jsonl" {
		t.Fatalf("AuditLogPath = %q, want custom path preserved", got.Mail.Pipeline.AuditLogPath)
	}
}

func TestConfigWithCredentialPrefersCredentialStoredIMAPUsernameOverInheritedFallback(t *testing.T) {
	t.Parallel()

	baseStateDir := t.TempDir()
	credentialStateDir := DefaultCredentialStateDir(baseStateDir, "archive")
	if err := SaveLocalConfig(credentialStateDir, LocalConfig{IMAPUsername: "archive@example.com"}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	cfg := Config{
		Auth: AuthConfig{
			ClientID:         "base-client",
			Tenant:           "base-tenant",
			AuthorityBaseURL: "https://authority.example.com",
			IMAPScopes:       []string{"imap.read"},
			StateDir:         baseStateDir,
			TokenStore:       "file",
		},
		Mail: MailConfig{
			Client: MailClientConfig{
				IMAPUsername: "base@example.com",
			},
		},
	}

	got := cfg.WithCredential("archive", Credential{
		Name: "archive",
		Kind: "oauth",
	})

	if got.Mail.Client.IMAPUsername != "archive@example.com" {
		t.Fatalf("IMAPUsername = %q, want archive@example.com", got.Mail.Client.IMAPUsername)
	}
}

func TestCredentialResolvedStateDirUsesExplicitStateDir(t *testing.T) {
	t.Parallel()

	credential := Credential{
		Name:     "archive",
		Kind:     "oauth",
		StateDir: "/custom/state",
	}
	if got, want := credential.ResolvedStateDir("/base", "archive"), "/custom/state"; got != want {
		t.Fatalf("ResolvedStateDir() = %q, want %q", got, want)
	}
}

func TestAuthConfigTokenPaths(t *testing.T) {
	t.Parallel()

	cfg := AuthConfig{StateDir: "/state"}
	if got := cfg.TokenPath(); got != "/state/token.json" {
		t.Fatalf("TokenPath() = %q, want /state/token.json", got)
	}
	if got := cfg.LegacyTokenPaths(); !reflect.DeepEqual(got, []string{"/state/graph-token.json"}) {
		t.Fatalf("LegacyTokenPaths() = %#v", got)
	}
	if got := cfg.TokenPaths(); !reflect.DeepEqual(got, []string{"/state/token.json", "/state/graph-token.json"}) {
		t.Fatalf("TokenPaths() = %#v", got)
	}
}

func TestAuthConfigValidateRejectsInvalidTokenStore(t *testing.T) {
	t.Parallel()

	cfg := AuthConfig{
		ClientID:         "client-id",
		Tenant:           "organizations",
		AuthorityBaseURL: "https://login.microsoftonline.com",
		IMAPScopes:       []string{"scope-imap"},
		StateDir:         "/state",
		TokenStore:       "invalid",
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "token store 不支持") {
		t.Fatalf("expected invalid token store error, got %v", err)
	}
}

func TestMailConfigValidateSync(t *testing.T) {
	t.Parallel()

	base := MailConfig{
		Client: MailClientConfig{
			GraphBaseURL: "https://graph.example.com/v1.0",
			EWSBaseURL:   "https://ews.example.com/EWS/Exchange.asmx",
			IMAPAddr:     "imap.example.com:993",
			IMAPUsername: "user@example.com",
		},
		Pipeline: MailPipelineConfig{
			OutputDir:         "output",
			SaveOutput:        true,
			ProtectSubject:    false,
			BackupDir:         "backup",
			AuditLogPath:      "audit.jsonl",
			WriteBackProvider: "ews",
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
			name: "missing ews base url when ews writeback enabled",
			mutate: func(cfg *MailConfig) {
				cfg.Client.EWSBaseURL = ""
			},
			wantErr: "ews base URL 不能为空",
		},
		{
			name: "missing imap username when imap writeback enabled",
			mutate: func(cfg *MailConfig) {
				cfg.Pipeline.WriteBackProvider = "imap"
				cfg.Client.IMAPUsername = ""
			},
			wantErr: "imap username 不能为空",
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
			name: "missing audit outputs",
			mutate: func(cfg *MailConfig) {
				cfg.Pipeline.AuditLogPath = ""
			},
			wantErr: "至少需要一个审计输出",
		},
		{
			name: "missing audit log path allowed when stdout enabled",
			mutate: func(cfg *MailConfig) {
				cfg.Pipeline.AuditLogPath = ""
				cfg.Pipeline.AuditStdout = true
			},
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
		{
			name: "unsupported writeback provider",
			mutate: func(cfg *MailConfig) {
				cfg.Pipeline.WriteBackProvider = "smtp"
			},
			wantErr: "write back provider 不支持",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := base
			tc.mutate(&cfg)

			err := cfg.ValidateSync()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSync() error = %v, want nil", err)
				}
				return
			}
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

func TestMailConfigFlowPathsSanitizeFolder(t *testing.T) {
	t.Parallel()

	cfg := MailConfig{
		Sync: MailSyncConfig{
			Folder:   "Archive/2026:April",
			StateDir: "/state",
		},
	}

	if got, want := cfg.FlowProducerStatePath(), filepath.Join("/state", "flow-sync-Archive_2026_April.json"); got != want {
		t.Fatalf("FlowProducerStatePath() = %q, want %q", got, want)
	}
	if got, want := cfg.FlowStateDir(), filepath.Join("/state", "flow-state", "Archive_2026_April"); got != want {
		t.Fatalf("FlowStateDir() = %q, want %q", got, want)
	}
}

func TestConfigBuildTopologyDefaultsToDiscardRoute(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider: "imap",
		Mail: MailConfig{
			Pipeline: MailPipelineConfig{
				OutputDir:  "/output",
				SaveOutput: false,
			},
			Sync: MailSyncConfig{
				Folder:       "INBOX",
				StateDir:     "/state",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
		},
	}

	topology, err := cfg.BuildTopology(TopologyOptions{})
	if err != nil {
		t.Fatalf("BuildTopology() error = %v", err)
	}

	if topology.DefaultSource != "default" || topology.DefaultRoute != "default" {
		t.Fatalf("unexpected defaults: %+v", topology)
	}
	if topology.DefaultCredential != "default" {
		t.Fatalf("DefaultCredential = %q, want default", topology.DefaultCredential)
	}
	credential, err := topology.DefaultCredentialConfig()
	if err != nil {
		t.Fatalf("DefaultCredentialConfig() error = %v", err)
	}
	if credential.Kind != "shared-session" {
		t.Fatalf("unexpected credential config: %+v", credential)
	}
	source, err := topology.DefaultSourceConfig()
	if err != nil {
		t.Fatalf("DefaultSourceConfig() error = %v", err)
	}
	if source.Mode != "poll" || source.StatePath != filepath.Join("/state", "flow-sync-imap-INBOX.json") {
		t.Fatalf("unexpected source config: %+v", source)
	}
	route, err := topology.DefaultRouteConfig()
	if err != nil {
		t.Fatalf("DefaultRouteConfig() error = %v", err)
	}
	if route.StateDir != filepath.Join("/state", "flow-state", "imap-INBOX") {
		t.Fatalf("unexpected route state dir: %q", route.StateDir)
	}
	if len(route.Targets) != 1 || route.Targets[0].SinkRef != "discard" {
		t.Fatalf("unexpected route targets: %+v", route.Targets)
	}
	if route.DeleteSource.Enabled {
		t.Fatalf("DeleteSource.Enabled = true, want false")
	}
}

func TestConfigBuildTopologyAddsWriteBackAndDeleteSourcePolicy(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider: "graph",
		Mail: MailConfig{
			Pipeline: MailPipelineConfig{
				OutputDir:         "/output",
				SaveOutput:        true,
				WriteBackProvider: "imap",
				WriteBackFolder:   "Encrypted",
			},
			Sync: MailSyncConfig{
				Folder:       "INBOX",
				StateDir:     "/state",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
		},
	}

	topology, err := cfg.BuildTopology(TopologyOptions{
		IncludeExisting: true,
		WriteBack:       true,
		VerifyWriteBack: true,
		DeleteSource:    true,
	})
	if err != nil {
		t.Fatalf("BuildTopology() error = %v", err)
	}

	source, err := topology.DefaultSourceConfig()
	if err != nil {
		t.Fatalf("DefaultSourceConfig() error = %v", err)
	}
	if !source.IncludeExisting || source.Driver != "graph" {
		t.Fatalf("unexpected source config: %+v", source)
	}
	writeBack, ok := topology.Sinks["write-back"]
	if !ok {
		t.Fatalf("missing write-back sink")
	}
	if writeBack.Driver != "imap" || writeBack.Folder != "Encrypted" || !writeBack.Verify {
		t.Fatalf("unexpected write-back sink: %+v", writeBack)
	}
	route, err := topology.DefaultRouteConfig()
	if err != nil {
		t.Fatalf("DefaultRouteConfig() error = %v", err)
	}
	if len(route.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(route.Targets))
	}
	if !route.DeleteSource.Enabled || !route.DeleteSource.RequireSameStore {
		t.Fatalf("unexpected delete-source policy: %+v", route.DeleteSource)
	}
	if got := route.DeleteSource.EligibleSinks; len(got) != 1 || got[0] != "write-back" {
		t.Fatalf("EligibleSinks = %+v, want [write-back]", got)
	}
}

func TestMailConfigFlowPathsForSourceIncludeDriverScope(t *testing.T) {
	t.Parallel()

	cfg := MailConfig{
		Sync: MailSyncConfig{
			Folder:   "Archive/2026:April",
			StateDir: "/state",
		},
	}

	if got, want := cfg.FlowProducerStatePathFor("default", "graph", "Archive/2026:April"), filepath.Join("/state", "flow-sync-graph-Archive_2026_April.json"); got != want {
		t.Fatalf("FlowProducerStatePathFor() = %q, want %q", got, want)
	}
	if got, want := cfg.FlowStateDirFor("default", "default", "imap", "Archive/2026:April"), filepath.Join("/state", "flow-state", "imap-Archive_2026_April"); got != want {
		t.Fatalf("FlowStateDirFor() = %q, want %q", got, want)
	}
	if got, want := cfg.FlowStateDirFor("archive", "office-source", "imap", "Archive/2026:April"), filepath.Join("/state", "flow-state", "archive-office-source-imap-Archive_2026_April"); got != want {
		t.Fatalf("FlowStateDirFor(custom) = %q, want %q", got, want)
	}
}

func TestConfigRunLockPathForIncludesSourceScope(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Auth: AuthConfig{StateDir: "/state"},
		Mail: MailConfig{
			Sync: MailSyncConfig{Folder: "INBOX"},
		},
	}

	if got, want := cfg.RunLockPathFor("office-source", "imap", "Archive/2026:April"), filepath.Join("/state", "run-office-source-imap-Archive_2026_April.lock"); got != want {
		t.Fatalf("RunLockPathFor() = %q, want %q", got, want)
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
		"MIMECRYPT_EWS_SCOPES",
		"MIMECRYPT_IMAP_SCOPES",
		"MIMECRYPT_STATE_DIR",
		"MIMECRYPT_TOPOLOGY_PATH",
		"MIMECRYPT_GRAPH_BASE_URL",
		"MIMECRYPT_EWS_BASE_URL",
		"MIMECRYPT_IMAP_ADDR",
		"MIMECRYPT_IMAP_USERNAME",
		"MIMECRYPT_OUTPUT_DIR",
		"MIMECRYPT_SAVE_OUTPUT",
		"MIMECRYPT_WORK_DIR",
		"MIMECRYPT_PROTECT_SUBJECT",
		"MIMECRYPT_BACKUP_DIR",
		"MIMECRYPT_BACKUP_KEY_ID",
		"MIMECRYPT_AUDIT_LOG_PATH",
		"MIMECRYPT_AUDIT_STDOUT",
		"MIMECRYPT_WRITEBACK_PROVIDER",
		"MIMECRYPT_FOLDER",
		"MIMECRYPT_WRITEBACK_FOLDER",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}
