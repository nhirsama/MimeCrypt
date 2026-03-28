package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordAppendsJSONL(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audit.jsonl")
	now := time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)
	service := Service{
		Path: path,
		Now: func() time.Time {
			return now
		},
	}

	if err := service.Record(Event{Event: "encrypted", MessageID: "m1"}); err != nil {
		t.Fatalf("Record() first error = %v", err)
	}
	if err := service.Record(Event{Event: "backup_saved", MessageID: "m1", BackupPath: "backup/file.pgp"}); err != nil {
		t.Fatalf("Record() second error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	lines := bytesToLines(content)
	if len(lines) != 2 {
		t.Fatalf("line count = %d, want 2", len(lines))
	}

	var first Event
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatalf("Unmarshal(first) error = %v", err)
	}
	if first.Timestamp != now {
		t.Fatalf("first timestamp = %s, want %s", first.Timestamp, now)
	}
	if first.Event != "encrypted" {
		t.Fatalf("first event = %q, want encrypted", first.Event)
	}

	var second Event
	if err := json.Unmarshal(lines[1], &second); err != nil {
		t.Fatalf("Unmarshal(second) error = %v", err)
	}
	if second.BackupPath != "backup/file.pgp" {
		t.Fatalf("second backup path = %q, want backup/file.pgp", second.BackupPath)
	}
}

func bytesToLines(content []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range content {
		if b != '\n' {
			continue
		}
		if i > start {
			lines = append(lines, append([]byte(nil), content[start:i]...))
		}
		start = i + 1
	}
	return lines
}
