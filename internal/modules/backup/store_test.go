package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/provider"
)

func TestSaveCiphertextWritesArmoredBackup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	message := provider.Message{
		ID:               "msg/1",
		ReceivedDateTime: time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC),
	}
	ciphertext := []byte("-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----\n")

	path, written, err := SaveCiphertext(dir, message, ciphertext)
	if err != nil {
		t.Fatalf("SaveCiphertext() error = %v", err)
	}

	if !strings.HasPrefix(filepath.Base(path), "20260328T100000Z_msg_1") {
		t.Fatalf("path = %q, unexpected file name", path)
	}
	if filepath.Ext(path) != ".pgp" {
		t.Fatalf("path ext = %q, want .pgp", filepath.Ext(path))
	}
	if written != int64(len(ciphertext)) {
		t.Fatalf("written = %d, want %d", written, len(ciphertext))
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(ciphertext) {
		t.Fatalf("ciphertext mismatch: got %q want %q", got, ciphertext)
	}
}
