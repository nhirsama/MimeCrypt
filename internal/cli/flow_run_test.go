package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/flowruntime"
	"mimecrypt/internal/mailflow"
)

var removedGlobalRuntimeFlags = []string{
	"client-id",
	"tenant",
	"state-dir",
	"authority-base-url",
	"graph-base-url",
	"ews-base-url",
	"imap-addr",
	"imap-username",
}

func TestRunCommandRemovesObsoleteMailflowFlags(t *testing.T) {
	t.Parallel()

	cmd := newRunCmd()
	for _, name := range []string{"topology-file", "source", "route", "once", "debug-save-first"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected %s flag on run command", name)
		}
	}
	for _, name := range []string{"delete-source", "write-back", "verify-write-back", "folder", "poll-interval", "cycle-timeout", "include-existing"} {
		if cmd.Flags().Lookup(name) != nil {
			t.Fatalf("did not expect removed flag %s on run command", name)
		}
	}
	for _, name := range removedGlobalRuntimeFlags {
		if cmd.Flags().Lookup(name) != nil {
			t.Fatalf("did not expect global runtime override flag %s on run command", name)
		}
	}
}

func TestProcessCommandExposesTransactionModeAndNoObsoleteFlags(t *testing.T) {
	t.Parallel()

	cmd := newProcessCmd()
	if cmd.Flags().Lookup("transaction-mode") == nil {
		t.Fatalf("expected transaction-mode flag on process command")
	}
	for _, name := range []string{"write-back", "verify-write-back", "folder"} {
		if cmd.Flags().Lookup(name) != nil {
			t.Fatalf("did not expect removed flag %s on process command", name)
		}
	}
	for _, name := range removedGlobalRuntimeFlags {
		if cmd.Flags().Lookup(name) != nil {
			t.Fatalf("did not expect global runtime override flag %s on process command", name)
		}
	}
}

func TestDownloadCommandExposesSourceTopologyFlagsOnly(t *testing.T) {
	t.Parallel()

	cmd := newDownloadCmd()
	for _, name := range []string{"topology-file", "source"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected %s flag on download command", name)
		}
	}
	if cmd.Flags().Lookup("route") != nil || cmd.Flags().Lookup("folder") != nil {
		t.Fatalf("did not expect route/folder flag on download command")
	}
	for _, name := range removedGlobalRuntimeFlags {
		if cmd.Flags().Lookup(name) != nil {
			t.Fatalf("did not expect global runtime override flag %s on download command", name)
		}
	}
}

func TestListCommandExposesSourceTopologyFlagsOnly(t *testing.T) {
	t.Parallel()

	cmd := newListCmd()
	for _, name := range []string{"topology-file", "source"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected %s flag on list command", name)
		}
	}
	if cmd.Flags().Lookup("route") != nil || cmd.Flags().Lookup("folder") != nil {
		t.Fatalf("did not expect route/folder flag on list command")
	}
	for _, name := range removedGlobalRuntimeFlags {
		if cmd.Flags().Lookup(name) != nil {
			t.Fatalf("did not expect global runtime override flag %s on list command", name)
		}
	}
}

func TestHealthCommandExposesTopologyFlags(t *testing.T) {
	t.Parallel()

	cmd := newHealthCmd()
	for _, name := range []string{"topology-file", "source", "route"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected %s flag on health command", name)
		}
	}
	for _, name := range removedGlobalRuntimeFlags {
		if cmd.Flags().Lookup(name) != nil {
			t.Fatalf("did not expect global runtime override flag %s on health command", name)
		}
	}
}

func TestLoginTokenRevokeCommandsExposeCredentialFlags(t *testing.T) {
	t.Parallel()

	for _, cmd := range []*cobra.Command{
		newLoginCmd(),
		newTokenStatusCmd(appconfig.Config{}),
		newRevokeCmd(),
	} {
		for _, name := range []string{"topology-file", "credential"} {
			if cmd.Flags().Lookup(name) == nil {
				t.Fatalf("expected %s flag on %s command", name, cmd.Name())
			}
		}
		for _, name := range removedGlobalRuntimeFlags {
			if cmd.Flags().Lookup(name) != nil {
				t.Fatalf("did not expect global runtime override flag %s on %s command", name, cmd.Name())
			}
		}
	}
}

func TestRevokeCommandExposesForceFlag(t *testing.T) {
	t.Parallel()

	if newRevokeCmd().Flags().Lookup("force") == nil {
		t.Fatalf("expected force flag on revoke command")
	}
}

func TestRootCommandDoesNotExposeRemovedFlowRunAlias(t *testing.T) {
	t.Parallel()

	root := newRootCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "flow-run" {
			t.Fatalf("unexpected removed command alias: %s", cmd.Name())
		}
		if cmd.Name() == "logout" {
			t.Fatalf("unexpected removed command alias: %s", cmd.Name())
		}
	}
}

func TestSummarizeMailflowResultUsesTraceAndDeliveries(t *testing.T) {
	t.Parallel()

	outputPath := filepath.Join(t.TempDir(), "out.eml")
	if err := os.WriteFile(outputPath, []byte("encrypted"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	summary, err := summarizeMailflowResult(mailflow.Result{
		Key: "tx-summary",
		Trace: mailflow.MailTrace{
			SourceMessageID: "m1",
			Attributes: map[string]string{
				"format":      "pgp-mime",
				"backup_path": "/backup/m1.pgp",
			},
		},
		Deliveries: map[string]mailflow.DeliveryReceipt{
			"archive-file-sink": {
				Consumer: "archive-file-sink",
				ID:       outputPath,
				Store: mailflow.StoreRef{
					Driver:  "file",
					Account: filepath.Dir(outputPath),
				},
			},
			"vault-remote-sink": {
				Consumer: "vault-remote-sink",
				Store: mailflow.StoreRef{
					Driver:  "imap",
					Account: "vault@example.com",
				},
				Verified: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("summarizeMailflowResult() error = %v", err)
	}
	if !summary.Encrypted || summary.AlreadyEncrypted {
		t.Fatalf("unexpected summary flags: %+v", summary)
	}
	if !summary.SavedOutput || summary.Path != outputPath {
		t.Fatalf("unexpected output summary: %+v", summary)
	}
	if !summary.WroteBack || !summary.Verified {
		t.Fatalf("unexpected write-back summary: %+v", summary)
	}
}

func TestSummarizeMailflowResultDoesNotTreatDiscardAsWriteBack(t *testing.T) {
	t.Parallel()

	summary, err := summarizeMailflowResult(mailflow.Result{
		Key: "tx-discard-summary",
		Trace: mailflow.MailTrace{
			SourceMessageID: "m-discard",
			Attributes: map[string]string{
				"format": "pgp-mime",
			},
		},
		Deliveries: map[string]mailflow.DeliveryReceipt{
			"discard-sink": {
				Consumer: "discard-sink",
				Verified: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("summarizeMailflowResult() error = %v", err)
	}
	if summary.SavedOutput || summary.WroteBack || summary.Verified {
		t.Fatalf("unexpected discard summary: %+v", summary)
	}
}

func TestSummarizeMailflowResultUsesBackupReceiptPath(t *testing.T) {
	t.Parallel()

	backupPath := filepath.Join(t.TempDir(), "backup.pgp")
	if err := os.WriteFile(backupPath, []byte("backup"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	summary, err := summarizeMailflowResult(mailflow.Result{
		Key: "tx-backup-summary",
		Trace: mailflow.MailTrace{
			SourceMessageID: "m-backup",
			Attributes: map[string]string{
				"format": "pgp-mime",
			},
		},
		Deliveries: map[string]mailflow.DeliveryReceipt{
			"backup": {
				Consumer: "archive-backup",
				ID:       backupPath,
				Store: mailflow.StoreRef{
					Driver:  "backup",
					Account: filepath.Dir(backupPath),
				},
				Verified: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("summarizeMailflowResult() error = %v", err)
	}
	if summary.BackupPath != backupPath {
		t.Fatalf("BackupPath = %q, want %q", summary.BackupPath, backupPath)
	}
	if summary.SavedOutput || summary.WroteBack || summary.Verified {
		t.Fatalf("unexpected backup summary flags: %+v", summary)
	}
}

func TestRunMailflowCycleSeparatesSkippedMessages(t *testing.T) {
	t.Parallel()

	runner := &fakeCycleRunner{
		results: []fakeCycleResult{
			{result: mailflow.Result{Skipped: false}, processed: true},
			{result: mailflow.Result{Skipped: true}, processed: true},
			{result: mailflow.Result{}, processed: false},
		},
	}

	processed, skipped, deleted, err := runMailflowCycle(context.Background(), time.Minute, runner)
	if err != nil {
		t.Fatalf("runMailflowCycle() error = %v", err)
	}
	if processed != 1 || skipped != 1 || deleted != 0 {
		t.Fatalf("unexpected counts: processed=%d skipped=%d deleted=%d", processed, skipped, deleted)
	}
}

func TestRunDebugSaveFirstRejectsPushSource(t *testing.T) {
	t.Parallel()

	err := runDebugSaveFirst(context.Background(), flowruntime.SourceRun{
		Source: appconfig.Source{
			Name:   "incoming",
			Driver: "webhook",
			Mode:   "push",
		},
	})
	if err == nil || err.Error() != "--debug-save-first 仅支持 mode=poll 的 source，当前 source=incoming mode=push" {
		t.Fatalf("runDebugSaveFirst() error = %v, want explicit push rejection", err)
	}
}

type fakeCycleRunner struct {
	results []fakeCycleResult
	index   int
}

type fakeCycleResult struct {
	result    mailflow.Result
	processed bool
	err       error
}

func (r *fakeCycleRunner) RunOnce(context.Context) (mailflow.Result, bool, error) {
	if r.index >= len(r.results) {
		return mailflow.Result{}, false, nil
	}
	current := r.results[r.index]
	r.index++
	return current.result, current.processed, current.err
}
