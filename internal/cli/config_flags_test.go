package cli

import (
	"testing"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

func TestBaseConfigFlagsApplyRebasesDefaultAuditLogPath(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Auth: appconfig.AuthConfig{
			ClientID:         "old-client",
			Tenant:           "old-tenant",
			AuthorityBaseURL: "https://old-authority",
			StateDir:         "/old-state",
		},
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{
				GraphBaseURL: "https://old-graph",
				EWSBaseURL:   "https://old-ews",
				IMAPAddr:     "old-imap:993",
				IMAPUsername: "old-user@example.com",
			},
			Pipeline: appconfig.MailPipelineConfig{AuditLogPath: appconfig.DefaultAuditLogPath("/old-state")},
			Sync:     appconfig.MailSyncConfig{StateDir: "/old-state"},
		},
	}

	flags := newBaseConfigFlags(cfg)
	flags.clientID = "new-client"
	flags.tenant = "new-tenant"
	flags.stateDir = "/new-state"
	flags.authorityBaseURL = "https://new-authority"
	flags.graphBaseURL = "https://new-graph"
	flags.ewsBaseURL = "https://new-ews"
	flags.imapAddr = "new-imap:993"
	flags.imapUsername = "new-user@example.com"

	cmd := &cobra.Command{Use: "test"}
	flags.addFlags(cmd)

	got := flags.apply(cfg, cmd)
	if got.Auth.ClientID != "new-client" || got.Auth.Tenant != "new-tenant" {
		t.Fatalf("unexpected auth config: %+v", got.Auth)
	}
	if got.Auth.StateDir != "/new-state" || got.Mail.Sync.StateDir != "/new-state" {
		t.Fatalf("unexpected state dir sync: auth=%q mail=%q", got.Auth.StateDir, got.Mail.Sync.StateDir)
	}
	if got.Mail.Client.IMAPUsername != "new-user@example.com" {
		t.Fatalf("IMAPUsername = %q, want new-user@example.com", got.Mail.Client.IMAPUsername)
	}
	if got.Mail.Pipeline.AuditLogPath != appconfig.DefaultAuditLogPath("/new-state") {
		t.Fatalf("AuditLogPath = %q, want %q", got.Mail.Pipeline.AuditLogPath, appconfig.DefaultAuditLogPath("/new-state"))
	}
}

func TestBaseConfigFlagsApplyUsesStoredIMAPUsernameForSelectedStateDir(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	if err := appconfig.SaveLocalConfig(stateDir, appconfig.LocalConfig{IMAPUsername: "saved@example.com"}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	cfg := appconfig.Config{
		Auth: appconfig.AuthConfig{StateDir: "/old-state"},
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{IMAPUsername: ""},
			Sync:   appconfig.MailSyncConfig{StateDir: "/old-state"},
		},
	}

	flags := newBaseConfigFlags(cfg)
	flags.stateDir = stateDir

	cmd := &cobra.Command{Use: "test"}
	flags.addFlags(cmd)

	got := flags.apply(cfg, cmd)
	if got.Mail.Client.IMAPUsername != "saved@example.com" {
		t.Fatalf("IMAPUsername = %q, want saved@example.com", got.Mail.Client.IMAPUsername)
	}
}

func TestBaseConfigFlagsApplyEnvOverridesStoredIMAPUsername(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("MIMECRYPT_IMAP_USERNAME", "env@example.com")
	if err := appconfig.SaveLocalConfig(stateDir, appconfig.LocalConfig{IMAPUsername: "saved@example.com"}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	cfg := appconfig.Config{
		Auth: appconfig.AuthConfig{StateDir: stateDir},
		Mail: appconfig.MailConfig{
			Sync: appconfig.MailSyncConfig{StateDir: stateDir},
		},
	}

	flags := newBaseConfigFlags(cfg)
	cmd := &cobra.Command{Use: "test"}
	flags.addFlags(cmd)

	got := flags.apply(cfg, cmd)
	if got.Mail.Client.IMAPUsername != "env@example.com" {
		t.Fatalf("IMAPUsername = %q, want env@example.com", got.Mail.Client.IMAPUsername)
	}
}

func TestPipelineConfigFlagsApply(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Mail: appconfig.MailConfig{
			Pipeline: appconfig.MailPipelineConfig{
				WorkDir:        "",
				ProtectSubject: false,
				BackupDir:      "backup",
				BackupKeyID:    "old-key",
				AuditLogPath:   "/old/audit.jsonl",
				AuditStdout:    false,
			},
		},
	}

	cmd := &cobra.Command{Use: "test"}
	flags := newPipelineConfigFlags(cfg)
	flags.addFlags(cmd)
	flags.workDir = "/new-work"
	flags.protectSubject = true
	flags.backupDir = "new-backup"
	flags.backupKeyID = "new-key"
	flags.auditStdout = true
	if err := cmd.Flags().Set("audit-log-path", "/new/audit.jsonl"); err != nil {
		t.Fatalf("Set(audit-log-path) error = %v", err)
	}

	got := flags.apply(cfg, cmd)
	if got.Mail.Pipeline.WorkDir != "/new-work" {
		t.Fatalf("WorkDir = %q, want /new-work", got.Mail.Pipeline.WorkDir)
	}
	if !got.Mail.Pipeline.ProtectSubject {
		t.Fatalf("ProtectSubject = false, want true")
	}
	if got.Mail.Pipeline.BackupDir != "new-backup" || got.Mail.Pipeline.BackupKeyID != "new-key" {
		t.Fatalf("unexpected backup config: %+v", got.Mail.Pipeline)
	}
	if got.Mail.Pipeline.AuditLogPath != "/new/audit.jsonl" {
		t.Fatalf("AuditLogPath = %q, want /new/audit.jsonl", got.Mail.Pipeline.AuditLogPath)
	}
	if !got.Mail.Pipeline.AuditStdout {
		t.Fatalf("AuditStdout = false, want true")
	}
}

func TestTopologyConfigFlagsApply(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{TopologyPath: "/old/topology.json"}
	flags := newTopologyConfigFlags(cfg)
	flags.topologyFile = "/new/topology.json"
	flags.sourceName = "office"
	flags.routeName = "archive"

	got := flags.apply(cfg)
	if got.TopologyPath != "/new/topology.json" {
		t.Fatalf("TopologyPath = %q, want /new/topology.json", got.TopologyPath)
	}
	if flags.sourceName != "office" || flags.routeName != "archive" {
		t.Fatalf("unexpected topology selection flags: %+v", flags)
	}
}

func TestCredentialConfigFlagsApply(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{TopologyPath: "/old/topology.json"}
	flags := newCredentialConfigFlags(cfg)
	flags.topologyFile = "/new/topology.json"
	flags.credentialName = "archive-auth"

	got := flags.apply(cfg)
	if got.TopologyPath != "/new/topology.json" {
		t.Fatalf("TopologyPath = %q, want /new/topology.json", got.TopologyPath)
	}
	if flags.credentialName != "archive-auth" {
		t.Fatalf("CredentialName = %q, want archive-auth", flags.credentialName)
	}
}
