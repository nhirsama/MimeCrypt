package cli

import (
	"testing"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

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
