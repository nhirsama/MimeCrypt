package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeRecipientSpecs(t *testing.T) {
	t.Parallel()

	got := normalizeRecipientSpecs([]string{
		"alice@example.com",
		"bob@example.com,carol@example.com",
		"  ",
		"0xDEADBEEF;alice@example.com",
		"FINGERPRINT123",
	})

	want := []string{
		"alice@example.com",
		"bob@example.com",
		"carol@example.com",
		"0xDEADBEEF",
		"FINGERPRINT123",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeRecipientSpecs() = %v, want %v", got, want)
	}
}

func TestBuildLocalEncryptServiceWithExplicitRecipients(t *testing.T) {
	t.Parallel()

	svc := buildLocalEncryptService([]string{"alice@example.com", "FPR123"}, "", false)
	if svc.RecipientResolver == nil {
		t.Fatalf("RecipientResolver should be set")
	}
	if svc.EnvLookup == nil {
		t.Fatalf("EnvLookup should be set")
	}

	recipients, err := svc.RecipientResolver(nil)
	if err != nil {
		t.Fatalf("RecipientResolver() error = %v", err)
	}
	wantRecipients := []string{"alice@example.com", "FPR123"}
	if !reflect.DeepEqual(recipients, wantRecipients) {
		t.Fatalf("RecipientResolver() = %v, want %v", recipients, wantRecipients)
	}
	if got := svc.EnvLookup(envPGPRecipientsKey); got != "alice@example.com,FPR123" {
		t.Fatalf("EnvLookup(%s) = %q", envPGPRecipientsKey, got)
	}
}

func TestBuildLocalEncryptServiceWithCustomGPGBinary(t *testing.T) {
	t.Setenv(envGPGBinaryKey, "gpg-from-env")
	svc := buildLocalEncryptService(nil, "/usr/local/bin/gpg", false)
	if svc.EnvLookup == nil {
		t.Fatalf("EnvLookup should be set")
	}
	if got := svc.EnvLookup(envGPGBinaryKey); got != "/usr/local/bin/gpg" {
		t.Fatalf("EnvLookup(%s) = %q", envGPGBinaryKey, got)
	}
}

func TestWriteSecureFileCreatesParentDir(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	path := filepath.Join(base, "nested", "encrypted.eml")
	content := []byte("cipher")

	if err := writeSecureFile(path, content, 0o600); err != nil {
		t.Fatalf("writeSecureFile() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "cipher" {
		t.Fatalf("file content = %q, want %q", string(got), "cipher")
	}
}
