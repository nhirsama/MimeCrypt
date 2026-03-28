package mimeutil

import (
	"strings"
	"testing"
)

func TestIsProcessedEncryptedBytes(t *testing.T) {
	t.Parallel()

	mimeBytes := []byte("X-MimeCrypt-Processed: yes\r\nContent-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"\r\n\r\nbody")
	if !IsProcessedEncryptedBytes(mimeBytes) {
		t.Fatalf("IsProcessedEncryptedBytes() = false, want true")
	}
}

func TestIsProcessedEncryptedStreamRejectsMissingMarker(t *testing.T) {
	t.Parallel()

	ok, err := IsProcessedEncryptedStream(strings.NewReader("Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"\r\n\r\nbody"))
	if err != nil {
		t.Fatalf("IsProcessedEncryptedStream() error = %v", err)
	}
	if ok {
		t.Fatalf("IsProcessedEncryptedStream() = true, want false")
	}
}

func TestIsProcessedEncryptedStreamReturnsParseError(t *testing.T) {
	t.Parallel()

	_, err := IsProcessedEncryptedStream(strings.NewReader("broken-header"))
	if err == nil {
		t.Fatalf("expected parse error")
	}
}
