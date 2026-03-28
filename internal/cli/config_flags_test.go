package cli

import (
	"testing"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

func TestProviderConfigFlagsApplyRebasesDefaultAuditLogPath(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Auth: appconfig.AuthConfig{
			ClientID:         "old-client",
			Tenant:           "old-tenant",
			AuthorityBaseURL: "https://old-authority",
			StateDir:         "/old-state",
		},
		Mail: appconfig.MailConfig{
			GraphBaseURL: "https://old-graph",
			StateDir:     "/old-state",
			AuditLogPath: appconfig.DefaultAuditLogPath("/old-state"),
		},
	}

	flags := newProviderConfigFlags(cfg)
	flags.clientID = "new-client"
	flags.tenant = "new-tenant"
	flags.stateDir = "/new-state"
	flags.authorityBaseURL = "https://new-authority"
	flags.graphBaseURL = "https://new-graph"

	got := flags.apply(cfg)
	if got.Auth.ClientID != "new-client" || got.Auth.Tenant != "new-tenant" {
		t.Fatalf("unexpected auth config: %+v", got.Auth)
	}
	if got.Auth.StateDir != "/new-state" || got.Mail.StateDir != "/new-state" {
		t.Fatalf("unexpected state dir sync: auth=%q mail=%q", got.Auth.StateDir, got.Mail.StateDir)
	}
	if got.Mail.GraphBaseURL != "https://new-graph" {
		t.Fatalf("GraphBaseURL = %q, want https://new-graph", got.Mail.GraphBaseURL)
	}
	if got.Mail.AuditLogPath != appconfig.DefaultAuditLogPath("/new-state") {
		t.Fatalf("AuditLogPath = %q, want %q", got.Mail.AuditLogPath, appconfig.DefaultAuditLogPath("/new-state"))
	}
}

func TestProcessingConfigFlagsApplyKeepsAuditLogPathWhenFlagNotChanged(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Mail: appconfig.MailConfig{
			OutputDir:    "output",
			SaveOutput:   false,
			BackupDir:    "backup",
			BackupKeyID:  "old-key",
			AuditLogPath: "/old/audit.jsonl",
		},
	}

	cmd := &cobra.Command{Use: "test"}
	flags := newProcessingConfigFlags(cfg)
	flags.addFlags(cmd)
	flags.outputDir = "new-output"
	flags.saveOutput = true
	flags.backupDir = "new-backup"
	flags.backupKeyID = "new-key"
	flags.auditLogPath = "/ignored/audit.jsonl"
	flags.writeBackFolder = "archive"

	got := flags.apply(cfg, cmd)
	if got.Mail.OutputDir != "new-output" || !got.Mail.SaveOutput {
		t.Fatalf("unexpected output config: %+v", got.Mail)
	}
	if got.Mail.BackupDir != "new-backup" || got.Mail.BackupKeyID != "new-key" {
		t.Fatalf("unexpected backup config: %+v", got.Mail)
	}
	if got.Mail.WriteBackFolder != "archive" {
		t.Fatalf("WriteBackFolder = %q, want archive", got.Mail.WriteBackFolder)
	}
	if got.Mail.AuditLogPath != "/old/audit.jsonl" {
		t.Fatalf("AuditLogPath = %q, want /old/audit.jsonl", got.Mail.AuditLogPath)
	}
}

func TestProcessingConfigFlagsApplyOverridesAuditLogPathWhenFlagChanged(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Mail: appconfig.MailConfig{
			AuditLogPath: "/old/audit.jsonl",
		},
	}

	cmd := &cobra.Command{Use: "test"}
	flags := newProcessingConfigFlags(cfg)
	flags.addFlags(cmd)
	if err := cmd.Flags().Set("audit-log-path", "/new/audit.jsonl"); err != nil {
		t.Fatalf("Set(audit-log-path) error = %v", err)
	}

	got := flags.apply(cfg, cmd)
	if got.Mail.AuditLogPath != "/new/audit.jsonl" {
		t.Fatalf("AuditLogPath = %q, want /new/audit.jsonl", got.Mail.AuditLogPath)
	}
}

func TestSyncConfigFlagsApply(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Mail: appconfig.MailConfig{
			Folder:       "inbox",
			PollInterval: time.Minute,
			CycleTimeout: 2 * time.Minute,
		},
	}

	flags := newSyncConfigFlags(cfg)
	flags.folder = "archive"
	flags.pollInterval = 30 * time.Second
	flags.cycleTimeout = 5 * time.Minute

	got := flags.apply(cfg)
	if got.Mail.Folder != "archive" {
		t.Fatalf("Folder = %q, want archive", got.Mail.Folder)
	}
	if got.Mail.PollInterval != 30*time.Second {
		t.Fatalf("PollInterval = %s, want 30s", got.Mail.PollInterval)
	}
	if got.Mail.CycleTimeout != 5*time.Minute {
		t.Fatalf("CycleTimeout = %s, want 5m", got.Mail.CycleTimeout)
	}
}
