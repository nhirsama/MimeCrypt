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
			Client: appconfig.MailClientConfig{
				GraphBaseURL: "https://old-graph",
				EWSBaseURL:   "https://old-ews",
			},
			Pipeline: appconfig.MailPipelineConfig{
				AuditLogPath: appconfig.DefaultAuditLogPath("/old-state"),
			},
			Sync: appconfig.MailSyncConfig{
				StateDir: "/old-state",
			},
		},
	}

	flags := newProviderConfigFlags(cfg)
	flags.clientID = "new-client"
	flags.tenant = "new-tenant"
	flags.stateDir = "/new-state"
	flags.authorityBaseURL = "https://new-authority"
	flags.graphBaseURL = "https://new-graph"
	flags.ewsBaseURL = "https://new-ews"

	got := flags.apply(cfg)
	if got.Auth.ClientID != "new-client" || got.Auth.Tenant != "new-tenant" {
		t.Fatalf("unexpected auth config: %+v", got.Auth)
	}
	if got.Auth.StateDir != "/new-state" || got.Mail.Sync.StateDir != "/new-state" {
		t.Fatalf("unexpected state dir sync: auth=%q mail=%q", got.Auth.StateDir, got.Mail.Sync.StateDir)
	}
	if got.Mail.Client.GraphBaseURL != "https://new-graph" {
		t.Fatalf("GraphBaseURL = %q, want https://new-graph", got.Mail.Client.GraphBaseURL)
	}
	if got.Mail.Client.EWSBaseURL != "https://new-ews" {
		t.Fatalf("EWSBaseURL = %q, want https://new-ews", got.Mail.Client.EWSBaseURL)
	}
	if got.Mail.Pipeline.AuditLogPath != appconfig.DefaultAuditLogPath("/new-state") {
		t.Fatalf("AuditLogPath = %q, want %q", got.Mail.Pipeline.AuditLogPath, appconfig.DefaultAuditLogPath("/new-state"))
	}
}

func TestProcessingConfigFlagsApplyKeepsAuditLogPathWhenFlagNotChanged(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Mail: appconfig.MailConfig{
			Pipeline: appconfig.MailPipelineConfig{
				OutputDir:         "output",
				SaveOutput:        false,
				BackupDir:         "backup",
				BackupKeyID:       "old-key",
				AuditLogPath:      "/old/audit.jsonl",
				WriteBackProvider: "ews",
			},
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
	flags.writeBackProvider = "graph"
	flags.writeBackFolder = "archive"

	got := flags.apply(cfg, cmd)
	if got.Mail.Pipeline.OutputDir != "new-output" || !got.Mail.Pipeline.SaveOutput {
		t.Fatalf("unexpected output config: %+v", got.Mail)
	}
	if got.Mail.Pipeline.BackupDir != "new-backup" || got.Mail.Pipeline.BackupKeyID != "new-key" {
		t.Fatalf("unexpected backup config: %+v", got.Mail)
	}
	if got.Mail.Pipeline.WriteBackFolder != "archive" {
		t.Fatalf("WriteBackFolder = %q, want archive", got.Mail.Pipeline.WriteBackFolder)
	}
	if got.Mail.Pipeline.WriteBackProvider != "graph" {
		t.Fatalf("WriteBackProvider = %q, want graph", got.Mail.Pipeline.WriteBackProvider)
	}
	if got.Mail.Pipeline.AuditLogPath != "/old/audit.jsonl" {
		t.Fatalf("AuditLogPath = %q, want /old/audit.jsonl", got.Mail.Pipeline.AuditLogPath)
	}
}

func TestProcessingConfigFlagsApplyOverridesAuditLogPathWhenFlagChanged(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Mail: appconfig.MailConfig{
			Pipeline: appconfig.MailPipelineConfig{
				AuditLogPath:      "/old/audit.jsonl",
				WriteBackProvider: "ews",
			},
		},
	}

	cmd := &cobra.Command{Use: "test"}
	flags := newProcessingConfigFlags(cfg)
	flags.addFlags(cmd)
	if err := cmd.Flags().Set("audit-log-path", "/new/audit.jsonl"); err != nil {
		t.Fatalf("Set(audit-log-path) error = %v", err)
	}

	got := flags.apply(cfg, cmd)
	if got.Mail.Pipeline.AuditLogPath != "/new/audit.jsonl" {
		t.Fatalf("AuditLogPath = %q, want /new/audit.jsonl", got.Mail.Pipeline.AuditLogPath)
	}
	if got.Mail.Pipeline.WriteBackProvider != "ews" {
		t.Fatalf("WriteBackProvider = %q, want ews", got.Mail.Pipeline.WriteBackProvider)
	}
}

func TestSyncConfigFlagsApply(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Mail: appconfig.MailConfig{
			Sync: appconfig.MailSyncConfig{
				Folder:       "inbox",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
		},
	}

	flags := newSyncConfigFlags(cfg)
	flags.folder = "archive"
	flags.pollInterval = 30 * time.Second
	flags.cycleTimeout = 5 * time.Minute

	got := flags.apply(cfg)
	if got.Mail.Sync.Folder != "archive" {
		t.Fatalf("Folder = %q, want archive", got.Mail.Sync.Folder)
	}
	if got.Mail.Sync.PollInterval != 30*time.Second {
		t.Fatalf("PollInterval = %s, want 30s", got.Mail.Sync.PollInterval)
	}
	if got.Mail.Sync.CycleTimeout != 5*time.Minute {
		t.Fatalf("CycleTimeout = %s, want 5m", got.Mail.Sync.CycleTimeout)
	}
}
