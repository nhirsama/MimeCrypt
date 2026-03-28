package fileutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicReplacesExistingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "payload.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	written, err := WriteFileAtomic(path, 0o600, &failAfterFirstReadReader{
		chunk: []byte("new payload"),
		err:   nil,
	})
	if err != nil {
		t.Fatalf("WriteFileAtomic() error = %v", err)
	}
	if written != int64(len("new payload")) {
		t.Fatalf("written = %d, want %d", written, len("new payload"))
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "new payload" {
		t.Fatalf("content = %q, want %q", string(got), "new payload")
	}
}

func TestWriteFileAtomicKeepsExistingFileOnCopyError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "payload.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	written, err := WriteFileAtomic(path, 0o600, &failAfterFirstReadReader{
		chunk: []byte("part"),
		err:   errors.New("boom"),
	})
	if err == nil {
		t.Fatalf("expected copy error")
	}
	if written != int64(len("part")) {
		t.Fatalf("written = %d, want %d", written, len("part"))
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(got) != "old" {
		t.Fatalf("content = %q, want %q", string(got), "old")
	}

	matches, globErr := filepath.Glob(filepath.Join(dir, ".payload.txt.tmp-*"))
	if globErr != nil {
		t.Fatalf("Glob() error = %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("unexpected temp files left behind: %v", matches)
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
		copy(p, r.chunk)
		if r.err == nil {
			return len(r.chunk), io.EOF
		}
		return len(r.chunk), nil
	}
	return 0, r.err
}
