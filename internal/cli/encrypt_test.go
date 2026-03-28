package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeExplicitRecipientEmails(t *testing.T) {
	t.Parallel()

	got, err := normalizeExplicitRecipientEmails([]string{
		"Alice <alice@example.com>",
		"bob@example.com,carol@example.com",
		"  ",
	})
	if err != nil {
		t.Fatalf("normalizeExplicitRecipientEmails() error = %v", err)
	}

	want := []string{
		"alice@example.com",
		"bob@example.com",
		"carol@example.com",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeExplicitRecipientEmails() = %v, want %v", got, want)
	}
}

func TestNormalizeExplicitRecipientEmailsRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	_, err := normalizeExplicitRecipientEmails([]string{"not-an-email"})
	if err == nil || !strings.Contains(err.Error(), "无效的收件人邮箱") {
		t.Fatalf("normalizeExplicitRecipientEmails() error = %v, want invalid email error", err)
	}
}

func TestNormalizeExplicitKeySpecs(t *testing.T) {
	t.Parallel()

	got, err := normalizeExplicitKeySpecs([]string{"0xDEADBEEF;FINGERPRINT123", "Alice Example <alice@example.com>"})
	if err != nil {
		t.Fatalf("normalizeExplicitKeySpecs() error = %v", err)
	}
	want := []string{"0xDEADBEEF", "FINGERPRINT123", "Alice Example <alice@example.com>"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeExplicitKeySpecs() = %v, want %v", got, want)
	}
}

func TestNormalizeExplicitKeySpecsRejectsOptionLikeValue(t *testing.T) {
	t.Parallel()

	_, err := normalizeExplicitKeySpecs([]string{"--homedir=/tmp/bad"})
	if err == nil || !strings.Contains(err.Error(), "不能以 '-' 开头") {
		t.Fatalf("normalizeExplicitKeySpecs() error = %v, want option-like value error", err)
	}
}

func TestBuildLocalEncryptServiceWithExplicitRecipientsAndKeys(t *testing.T) {
	t.Parallel()

	svc, err := buildLocalEncryptService([]string{"alice@example.com"}, []string{"FPR123"}, "", false)
	if err != nil {
		t.Fatalf("buildLocalEncryptService() error = %v", err)
	}
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
	svc, err := buildLocalEncryptService(nil, nil, "/usr/local/bin/gpg", false)
	if err != nil {
		t.Fatalf("buildLocalEncryptService() error = %v", err)
	}
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
