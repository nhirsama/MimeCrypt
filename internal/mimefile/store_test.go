package mimefile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mimecrypt/internal/provider"
)

func TestSaveBytesToOutputDirWritesContentAndPermissions(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "mime")
	message := provider.Message{
		ID:               "msg/1",
		ReceivedDateTime: time.Date(2026, 3, 28, 12, 34, 56, 0, time.UTC),
	}
	mimeBytes := []byte("From: alice@example.com\r\n\r\nhello")

	path, written, err := SaveBytesToOutputDir(outputDir, message, mimeBytes)
	if err != nil {
		t.Fatalf("SaveBytesToOutputDir() error = %v", err)
	}
	if filepath.Base(path) != "20260328T123456Z_msg_1.eml" {
		t.Fatalf("path base = %q, want 20260328T123456Z_msg_1.eml", filepath.Base(path))
	}
	if written != int64(len(mimeBytes)) {
		t.Fatalf("written = %d, want %d", written, len(mimeBytes))
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(mimeBytes) {
		t.Fatalf("content mismatch: got %q want %q", got, mimeBytes)
	}

	dirInfo, err := os.Stat(outputDir)
	if err != nil {
		t.Fatalf("Stat(outputDir) error = %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("output dir perm = %o, want 700", dirInfo.Mode().Perm())
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(path) error = %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file perm = %o, want 600", fileInfo.Mode().Perm())
	}
}

func TestSaveToOutputDirReturnsCopyError(t *testing.T) {
	t.Parallel()

	message := provider.Message{ID: "msg-1"}
	reader := &failAfterFirstReadReader{err: errors.New("boom")}

	path, written, err := SaveToOutputDir(t.TempDir(), message, reader)
	if err == nil || path != "" {
		t.Fatalf("expected write error with empty path, got path=%q err=%v", path, err)
	}
	if written != int64(len(reader.chunk)) {
		t.Fatalf("written = %d, want %d", written, len(reader.chunk))
	}
}

func TestBuildMessageFileStemUsesFallbacks(t *testing.T) {
	t.Parallel()

	if got := BuildMessageFileStem(provider.Message{}); got != "message_unknown" {
		t.Fatalf("BuildMessageFileStem(empty) = %q, want message_unknown", got)
	}

	got := BuildMessageFileStem(provider.Message{
		ID:               " /strange<>id/ ",
		ReceivedDateTime: time.Date(2026, 3, 28, 1, 2, 3, 0, time.UTC),
	})
	if got != "20260328T010203Z_strange__id" {
		t.Fatalf("BuildMessageFileStem() = %q, want 20260328T010203Z_strange__id", got)
	}
}

type failAfterFirstReadReader struct {
	chunk []byte
	read  bool
	err   error
}

func (r *failAfterFirstReadReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		r.chunk = []byte("part")
		copy(p, r.chunk)
		return len(r.chunk), nil
	}
	return 0, r.err
}
