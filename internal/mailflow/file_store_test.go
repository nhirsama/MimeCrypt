package mailflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStateStoreSaveAndLoad(t *testing.T) {
	t.Parallel()

	store := FileStateStore{Dir: t.TempDir()}
	want := TxState{
		Key: "tx-save-load",
		Trace: MailTrace{
			TransactionKey: "tx-save-load",
			SourceName:     "office_inbox",
		},
		Plan: ExecutionPlan{
			Targets: []DeliveryTarget{{
				Name:     "archive-main",
				Consumer: "archive",
				Required: true,
			}},
		},
		Deliveries: map[string]DeliveryReceipt{
			"archive-main": {
				Target:   "archive-main",
				Consumer: "archive",
				ID:       "msg-1",
			},
		},
		Completed: true,
	}

	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, found, err := store.Load(context.Background(), "tx-save-load")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !found {
		t.Fatalf("Load() found = false, want true")
	}
	if got.Key != want.Key || got.Trace.SourceName != want.Trace.SourceName || !got.Completed {
		t.Fatalf("loaded state mismatch: %+v", got)
	}
	if got.Deliveries["archive-main"].ID != "msg-1" {
		t.Fatalf("delivery id = %q, want msg-1", got.Deliveries["archive-main"].ID)
	}
}

func TestFileStateStoreReturnsNotFoundForMissingKey(t *testing.T) {
	t.Parallel()

	store := FileStateStore{Dir: t.TempDir()}
	_, found, err := store.Load(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if found {
		t.Fatalf("Load() found = true, want false")
	}
}

func TestFileStateStoreUsesStableHashedPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := FileStateStore{Dir: dir}
	if err := store.Save(context.Background(), TxState{Key: "tx-hash"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("state file ext = %q, want .json", filepath.Ext(entries[0].Name()))
	}
}
