package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
)

func TestValidateMailflowFlagsRejectsDeleteSourceWithoutWriteBack(t *testing.T) {
	t.Parallel()

	err := validateMailflowFlags(true, false, false, true, "")
	if err == nil || !strings.Contains(err.Error(), "--delete-source 依赖 --write-back") {
		t.Fatalf("validateMailflowFlags() error = %v, want delete-source validation", err)
	}
}

func TestBuildMailflowPlanAddsConfiguredTargets(t *testing.T) {
	t.Parallel()

	plan, err := buildMailflowPlan(true, true, true)
	if err != nil {
		t.Fatalf("buildMailflowPlan() error = %v", err)
	}
	if len(plan.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(plan.Targets))
	}
	if !plan.DeleteSource.Enabled {
		t.Fatalf("DeleteSource.Enabled = false, want true")
	}
	if got := plan.DeleteSource.EligibleConsumers; len(got) != 1 || got[0] != "write-back" {
		t.Fatalf("EligibleConsumers = %+v, want [write-back]", got)
	}
}

func TestBuildMailflowPlanFallsBackToDiscardTarget(t *testing.T) {
	t.Parallel()

	plan, err := buildMailflowPlan(false, false, false)
	if err != nil {
		t.Fatalf("buildMailflowPlan() error = %v", err)
	}
	if len(plan.Targets) != 1 {
		t.Fatalf("len(Targets) = %d, want 1", len(plan.Targets))
	}
	if plan.Targets[0].Consumer != "discard" {
		t.Fatalf("Consumer = %q, want discard", plan.Targets[0].Consumer)
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
			"local-output": {
				Consumer: "local-output",
				ID:       outputPath,
			},
			"write-back": {
				Consumer: "write-back",
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
	if summary.BackupPath != "/backup/m1.pgp" || summary.Format != "pgp-mime" {
		t.Fatalf("unexpected trace summary: %+v", summary)
	}
}

func TestSummarizeMailflowResultMarksAlreadyEncryptedSkip(t *testing.T) {
	t.Parallel()

	summary, err := summarizeMailflowResult(mailflow.Result{
		Key:     "tx-skip",
		Skipped: true,
		Trace: mailflow.MailTrace{
			SourceMessageID: "m2",
			Attributes: map[string]string{
				"already_encrypted": "true",
				"format":            "pgp-mime",
			},
		},
	})
	if err != nil {
		t.Fatalf("summarizeMailflowResult() error = %v", err)
	}
	if !summary.AlreadyEncrypted || summary.Encrypted {
		t.Fatalf("unexpected summary flags: %+v", summary)
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

	processed, skipped, deleted, err := runMailflowCycle(context.Background(), appconfig.Config{
		Mail: appconfig.MailConfig{
			Sync: appconfig.MailSyncConfig{CycleTimeout: time.Minute},
		},
	}, runner)
	if err != nil {
		t.Fatalf("runMailflowCycle() error = %v", err)
	}
	if processed != 1 || skipped != 1 || deleted != 0 {
		t.Fatalf("unexpected counts: processed=%d skipped=%d deleted=%d", processed, skipped, deleted)
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
