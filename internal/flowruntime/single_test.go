package flowruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
)

func TestStateStoreForModeUsesPersistentFileStore(t *testing.T) {
	t.Parallel()

	store, err := stateStoreForMode(testSingleSourceRun(t), TransactionModePersistent)
	if err != nil {
		t.Fatalf("stateStoreForMode() error = %v", err)
	}
	fileStore, ok := store.(mailflow.FileStateStore)
	if !ok {
		t.Fatalf("store type = %T, want mailflow.FileStateStore", store)
	}
	if fileStore.Dir == "" {
		t.Fatalf("FileStateStore.Dir = empty")
	}
}

func TestStateStoreForModeUsesEphemeralMemoryStore(t *testing.T) {
	t.Parallel()

	store, err := stateStoreForMode(testSingleSourceRun(t), TransactionModeEphemeral)
	if err != nil {
		t.Fatalf("stateStoreForMode() error = %v", err)
	}
	if _, ok := store.(*mailflow.MemoryStateStore); !ok {
		t.Fatalf("store type = %T, want *mailflow.MemoryStateStore", store)
	}
}

func TestBuildCoordinatorForModeRejectsUnknownMode(t *testing.T) {
	t.Parallel()

	_, err := buildCoordinatorForMode(context.Background(), testSingleSourceRun(t), TransactionMode("unknown"))
	if err == nil || !strings.Contains(err.Error(), "不支持的事务模式") {
		t.Fatalf("buildCoordinatorForMode() error = %v, want unsupported mode error", err)
	}
}

func TestBuildCoordinatorForModeBindsSelectedStoreType(t *testing.T) {
	t.Parallel()

	persistent, err := buildCoordinatorForMode(context.Background(), testSingleSourceRun(t), TransactionModePersistent)
	if err != nil {
		t.Fatalf("buildCoordinatorForMode(persistent) error = %v", err)
	}
	if _, ok := persistent.Store.(mailflow.FileStateStore); !ok {
		t.Fatalf("persistent store type = %T, want mailflow.FileStateStore", persistent.Store)
	}

	ephemeral, err := buildCoordinatorForMode(context.Background(), testSingleSourceRun(t), TransactionModeEphemeral)
	if err != nil {
		t.Fatalf("buildCoordinatorForMode(ephemeral) error = %v", err)
	}
	if _, ok := ephemeral.Store.(*mailflow.MemoryStateStore); !ok {
		t.Fatalf("ephemeral store type = %T, want *mailflow.MemoryStateStore", ephemeral.Store)
	}
}

func testSingleSourceRun(t *testing.T) SourceRun {
	t.Helper()

	stateDir := t.TempDir()
	source := appconfig.Source{
		Name:         "office",
		Driver:       "imap",
		Mode:         "poll",
		Folder:       "INBOX",
		StatePath:    filepath.Join(stateDir, "flow-sync-office-imap-INBOX.json"),
		PollInterval: time.Minute,
		CycleTimeout: 2 * time.Minute,
	}
	route := appconfig.Route{
		Name:       "default",
		StateDir:   filepath.Join(stateDir, "flow-state", "default-office-imap-INBOX"),
		SourceRefs: []string{"office"},
		Targets: []appconfig.RouteTarget{
			{Name: "discard-primary", SinkRef: "discard", Artifact: "primary", Required: true},
		},
	}
	return SourceRun{
		Source: source,
		Route:  route,
		Config: appconfig.Config{
			Mail: appconfig.MailConfig{
				Pipeline: appconfig.MailPipelineConfig{
					AuditLogPath: filepath.Join(stateDir, "audit.jsonl"),
				},
			},
		},
		Sinks: map[string]SinkPlan{
			"discard": {
				Sink: appconfig.Sink{Name: "discard", Driver: "discard"},
			},
		},
	}
}
