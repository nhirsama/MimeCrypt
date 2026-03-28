package encrypt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
)

type fakeEncryptor struct {
	gotRecipients []string
	gotMIME       []byte
	output        []byte
	err           error
}

func (f *fakeEncryptor) Encrypt(_ context.Context, mimeBytes []byte, recipients []string) ([]byte, error) {
	f.gotRecipients = append([]string(nil), recipients...)
	f.gotMIME = append([]byte(nil), mimeBytes...)
	if f.err != nil {
		return nil, f.err
	}
	return append([]byte(nil), f.output...), nil
}

type fakeStreamingEncryptor struct {
	gotRecipients []string
	gotMIME       []byte
	output        []byte
	err           error
}

func (f *fakeStreamingEncryptor) Encrypt(context.Context, []byte, []string) ([]byte, error) {
	return nil, fmt.Errorf("unexpected non-streaming Encrypt call")
}

func (f *fakeStreamingEncryptor) EncryptTo(_ context.Context, mimeBytes []byte, recipients []string, out io.Writer) error {
	f.gotRecipients = append([]string(nil), recipients...)
	f.gotMIME = append([]byte(nil), mimeBytes...)
	if f.err != nil {
		return f.err
	}
	_, err := out.Write(f.output)
	return err
}

func TestRunReturnsErrAlreadyEncryptedForPGPMIME(t *testing.T) {
	t.Parallel()

	service := Service{}
	_, err := service.Run([]byte("Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"\r\n\r\nbody"))
	if !errors.Is(err, ErrAlreadyEncrypted) {
		t.Fatalf("expected ErrAlreadyEncrypted, got %v", err)
	}
}

func TestRunReturnsErrAlreadyEncryptedForInlinePGP(t *testing.T) {
	t.Parallel()

	service := Service{}
	_, err := service.Run([]byte("hello\n-----BEGIN PGP MESSAGE-----\nabc"))
	if !errors.Is(err, ErrAlreadyEncrypted) {
		t.Fatalf("expected ErrAlreadyEncrypted, got %v", err)
	}
}

func TestRunReturnsErrAlreadyEncryptedForSMIME(t *testing.T) {
	t.Parallel()

	service := Service{}
	_, err := service.Run([]byte("Content-Type: application/pkcs7-mime; smime-type=enveloped-data; name=\"smime.p7m\"\r\n\r\nbinary"))
	if !errors.Is(err, ErrAlreadyEncrypted) {
		t.Fatalf("expected ErrAlreadyEncrypted, got %v", err)
	}
}

func TestRunEncryptsPlainMIMEToPGPMIME(t *testing.T) {
	t.Parallel()

	encryptor := &fakeEncryptor{
		output: []byte("-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----\n"),
	}

	service := Service{
		Encryptor: encryptor,
		EnvLookup: func(key string) string {
			if key == "MIMECRYPT_PGP_RECIPIENTS" {
				return "ops@example.com; Alice <alice@example.com>"
			}
			return ""
		},
	}

	input := []byte(
		"From: sender@example.com\r\n" +
			"To: alice@example.com\r\n" +
			"Cc: bob@example.com\r\n" +
			"Subject: hello\r\n" +
			"Date: Thu, 02 Jan 2026 10:00:00 +0000\r\n" +
			"Message-ID: <m1@example.com>\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n" +
			"\r\n" +
			"hello world\r\n",
	)

	result, err := service.Run(input)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Encrypted || result.AlreadyEncrypted || result.Format != "pgp-mime" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !bytes.Contains(result.Armored, []byte("-----BEGIN PGP MESSAGE-----")) {
		t.Fatalf("missing armored payload in result")
	}

	expectedRecipients := []string{"alice@example.com", "bob@example.com", "ops@example.com"}
	if !slices.Equal(encryptor.gotRecipients, expectedRecipients) {
		t.Fatalf("unexpected recipients: got=%v want=%v", encryptor.gotRecipients, expectedRecipients)
	}
	if !bytes.Equal(encryptor.gotMIME, input) {
		t.Fatalf("encryptor input was changed unexpectedly")
	}

	if !bytes.Contains(result.MIME, []byte("Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"")) {
		t.Fatalf("missing pgp-mime content type in output")
	}
	if !bytes.Contains(result.MIME, []byte("X-MimeCrypt-Processed: yes")) {
		t.Fatalf("missing processed marker header in output")
	}
	if !bytes.Contains(result.MIME, []byte("Content-Type: application/pgp-encrypted")) {
		t.Fatalf("missing application/pgp-encrypted part in output")
	}
	if !bytes.Contains(result.MIME, []byte("Version: 1")) {
		t.Fatalf("missing pgp version part")
	}
	if !bytes.Contains(result.MIME, []byte("-----BEGIN PGP MESSAGE-----")) {
		t.Fatalf("missing armored payload in output")
	}
	if !bytes.Contains(result.MIME, []byte("Subject: hello")) {
		t.Fatalf("expected original mail headers to be preserved")
	}
}

func TestRunUsesStreamingEncryptorWhenAvailable(t *testing.T) {
	t.Parallel()

	encryptor := &fakeStreamingEncryptor{
		output: []byte("-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----\n"),
	}

	service := Service{
		Encryptor: encryptor,
		EnvLookup: func(key string) string {
			if key == "MIMECRYPT_PGP_RECIPIENTS" {
				return "ops@example.com"
			}
			return ""
		},
	}

	input := []byte(
		"From: sender@example.com\r\n" +
			"To: alice@example.com\r\n" +
			"Subject: hello\r\n" +
			"Date: Thu, 02 Jan 2026 10:00:00 +0000\r\n" +
			"Message-ID: <m1@example.com>\r\n" +
			"\r\n" +
			"hello world\r\n",
	)

	result, err := service.Run(input)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !bytes.Equal(encryptor.gotMIME, input) {
		t.Fatalf("streaming encryptor input was changed unexpectedly")
	}
	if !slices.Equal(encryptor.gotRecipients, []string{"alice@example.com", "ops@example.com"}) {
		t.Fatalf("unexpected recipients: %v", encryptor.gotRecipients)
	}
	if !bytes.Contains(result.MIME, []byte("-----BEGIN PGP MESSAGE-----")) {
		t.Fatalf("missing armored payload in MIME output")
	}
	if !bytes.Equal(result.Armored, encryptor.output) {
		t.Fatalf("unexpected armored result: %q", result.Armored)
	}
}

func TestRunProtectSubjectWritesOuterPlaceholderOnly(t *testing.T) {
	t.Parallel()

	encryptor := &fakeEncryptor{
		output: []byte("-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----\n"),
	}
	service := Service{
		Encryptor:      encryptor,
		ProtectSubject: true,
		RecipientResolver: func([]byte) ([]string, error) {
			return []string{"alice@example.com"}, nil
		},
	}

	input := []byte(
		"From: sender@example.com\r\n" +
			"To: alice@example.com\r\n" +
			"Subject: Secret Subject\r\n" +
			"Date: Thu, 02 Jan 2026 10:00:00 +0000\r\n" +
			"Message-ID: <m1@example.com>\r\n" +
			"\r\n" +
			"hello world\r\n",
	)

	result, err := service.Run(input)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !bytes.Equal(encryptor.gotMIME, input) {
		t.Fatalf("encryptor input was changed unexpectedly")
	}
	if !bytes.Contains(result.MIME, []byte("Subject: ...")) {
		t.Fatalf("expected outer placeholder subject in output")
	}
	if bytes.Contains(result.MIME, []byte("Subject: Secret Subject")) {
		t.Fatalf("outer MIME should not expose original subject")
	}
}

func TestRunPlainMIMEWithoutRecipientsReturnsError(t *testing.T) {
	t.Parallel()

	service := Service{
		Encryptor: &fakeEncryptor{
			output: []byte("unused"),
		},
		EnvLookup: func(string) string { return "" },
	}

	_, err := service.Run([]byte("From: sender@example.com\r\nSubject: no recipients\r\n\r\nhello"))
	if !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("expected ErrNoRecipients, got %v", err)
	}
}

func TestRunUsesRecipientResolverOverride(t *testing.T) {
	t.Parallel()

	encryptor := &fakeEncryptor{
		output: []byte("-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----\n"),
	}
	service := Service{
		Encryptor: encryptor,
		RecipientResolver: func([]byte) ([]string, error) {
			return []string{"TESTKEY123"}, nil
		},
	}

	input := []byte(
		"From: sender@example.com\r\n" +
			"To: alice@example.com\r\n" +
			"Subject: hello\r\n" +
			"\r\n" +
			"hello world\r\n",
	)

	if _, err := service.Run(input); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !slices.Equal(encryptor.gotRecipients, []string{"TESTKEY123"}) {
		t.Fatalf("unexpected recipients: %v", encryptor.gotRecipients)
	}
}

func TestRunRecipientResolverErrorIsPropagated(t *testing.T) {
	t.Parallel()

	wantErr := fmt.Errorf("resolver failed")
	service := Service{
		RecipientResolver: func([]byte) ([]string, error) {
			return nil, wantErr
		},
	}

	_, err := service.Run([]byte("From: sender@example.com\r\nSubject: test\r\n\r\nhello"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected resolver error, got %v", err)
	}
}

func TestRunContextUsesEnvLookupForGPGBinary(t *testing.T) {
	t.Parallel()

	service := Service{
		EnvLookup: func(key string) string {
			switch key {
			case envGPGBinary:
				return "/definitely/not/found/gpg"
			case envPGPRecipients:
				return "alice@example.com"
			default:
				return ""
			}
		},
	}

	_, err := service.RunContext(context.Background(), []byte(
		"From: sender@example.com\r\n"+
			"To: alice@example.com\r\n"+
			"Subject: hello\r\n"+
			"\r\n"+
			"hello world\r\n",
	))
	if err == nil || !strings.Contains(err.Error(), "/definitely/not/found/gpg") {
		t.Fatalf("expected custom gpg binary error, got %v", err)
	}
}

func TestValidateRecipientSpecRejectsOptionLikeValue(t *testing.T) {
	t.Parallel()

	err := ValidateRecipientSpec("--homedir=/tmp/bad")
	if err == nil || !strings.Contains(err.Error(), "不能以 '-' 开头") {
		t.Fatalf("ValidateRecipientSpec() error = %v, want option-like validation error", err)
	}
}

func TestParseAddressListDropsInvalidTokens(t *testing.T) {
	t.Parallel()

	got := parseAddressList("alice@example.com, --homedir=/tmp/bad, bob@example.com")
	want := []string{"alice@example.com", "bob@example.com"}
	if !slices.Equal(got, want) {
		t.Fatalf("parseAddressList() = %v, want %v", got, want)
	}
}
