package encrypt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type parsedPart struct {
	ContentType string
	Params      map[string]string
	Body        []byte
	RawHeader   map[string][]string
}

func TestDetectFormatTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantFmt   string
		encrypted bool
	}{
		{
			name:      "pgp mime header",
			input:     "Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"\r\n\r\nx",
			wantFmt:   "pgp-mime",
			encrypted: true,
		},
		{
			name:      "pgp mime header mixed case",
			input:     "Content-Type: MULTIPART/ENCRYPTED; PROTOCOL=\"APPLICATION/PGP-ENCRYPTED\"\r\n\r\nx",
			wantFmt:   "pgp-mime",
			encrypted: true,
		},
		{
			name:      "inline pgp body",
			input:     "hello\n-----BEGIN PGP MESSAGE-----\nabc\n",
			wantFmt:   "inline-pgp",
			encrypted: true,
		},
		{
			name:      "smime enveloped data",
			input:     "Content-Type: application/pkcs7-mime; smime-type=enveloped-data; name=\"smime.p7m\"\r\n\r\nx",
			wantFmt:   "smime-enveloped",
			encrypted: true,
		},
		{
			name:      "smime p7m filename",
			input:     "Content-Type: application/x-pkcs7-mime; name=\"smime.p7m\"\r\nContent-Disposition: attachment; filename=\"smime.p7m\"\r\n\r\nx",
			wantFmt:   "smime-enveloped",
			encrypted: true,
		},
		{
			name:      "smime signed not encrypted",
			input:     "Content-Type: multipart/signed; protocol=\"application/pkcs7-signature\"\r\n\r\nx",
			wantFmt:   "plain",
			encrypted: false,
		},
		{
			name:      "plain message",
			input:     "From: a@example.com\r\nTo: b@example.com\r\n\r\nhello",
			wantFmt:   "plain",
			encrypted: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotFmt, gotEncrypted := detectFormat([]byte(tt.input))
			if gotFmt != tt.wantFmt || gotEncrypted != tt.encrypted {
				t.Fatalf("detectFormat() = (%s, %t), want (%s, %t)", gotFmt, gotEncrypted, tt.wantFmt, tt.encrypted)
			}
		})
	}
}

func TestAlreadyEncryptedErrorCarriesFormat(t *testing.T) {
	t.Parallel()

	service := Service{}
	_, err := service.Run([]byte("Content-Type: application/pkcs7-mime; smime-type=enveloped-data; name=\"smime.p7m\"\r\n\r\nx"))
	if !errors.Is(err, ErrAlreadyEncrypted) {
		t.Fatalf("expected ErrAlreadyEncrypted, got %v", err)
	}

	var typed AlreadyEncryptedError
	if !errors.As(err, &typed) {
		t.Fatalf("expected AlreadyEncryptedError, got %T", err)
	}
	if typed.Format != "smime-enveloped" {
		t.Fatalf("unexpected format: %s", typed.Format)
	}
	if !strings.Contains(err.Error(), "smime-enveloped") {
		t.Fatalf("error should include format: %v", err)
	}
}

func TestRunAlreadyEncryptedDoesNotInvokeEncryptor(t *testing.T) {
	t.Parallel()

	encryptor := &fakeEncryptor{
		output: []byte("unused"),
	}
	service := Service{Encryptor: encryptor}

	_, err := service.Run([]byte("hello\n-----BEGIN PGP MESSAGE-----\nabc"))
	if !errors.Is(err, ErrAlreadyEncrypted) {
		t.Fatalf("expected ErrAlreadyEncrypted, got %v", err)
	}
	if len(encryptor.gotRecipients) != 0 || len(encryptor.gotMIME) != 0 {
		t.Fatalf("encryptor should not be called for already encrypted input")
	}
}

func TestCollectRecipientsFromEnvSupportsMixedSeparators(t *testing.T) {
	t.Parallel()

	got := collectRecipientsFromEnv("Alice <alice@example.com>;bob@example.com\ncarol@example.com,dave@example.com")
	want := []string{"alice@example.com", "bob@example.com", "carol@example.com", "dave@example.com"}
	if !slices.Equal(got, want) {
		t.Fatalf("collectRecipientsFromEnv() = %v, want %v", got, want)
	}
}

func TestParseAddressListFallbackOnInvalidInput(t *testing.T) {
	t.Parallel()

	got := parseAddressList("broken-address-list@@;B@example.com test@example.com")
	want := []string{"broken-address-list@@", "b@example.com", "test@example.com"}
	if !slices.Equal(got, want) {
		t.Fatalf("parseAddressList() = %v, want %v", got, want)
	}
}

func TestDedupeRecipientsSortsAndNormalizes(t *testing.T) {
	t.Parallel()

	got := dedupeRecipients([]string{" B@example.com ", "a@example.com", "b@example.com", "A@example.com", ""})
	want := []string{"a@example.com", "b@example.com"}
	if !slices.Equal(got, want) {
		t.Fatalf("dedupeRecipients() = %v, want %v", got, want)
	}
}

func TestResolveRecipientsFromHeadersAndEnv(t *testing.T) {
	t.Parallel()

	service := Service{
		EnvLookup: func(key string) string {
			if key == "MIMECRYPT_PGP_RECIPIENTS" {
				return "ops@example.com"
			}
			return ""
		},
	}

	input := []byte(
		"From: sender@example.com\r\n" +
			"To: Alice <alice@example.com>\r\n" +
			"Cc: bob@example.com\r\n" +
			"Bcc: hidden@example.com\r\n" +
			"Subject: hi\r\n" +
			"\r\n" +
			"hello\r\n",
	)

	got, err := service.resolveRecipients(input)
	if err != nil {
		t.Fatalf("resolveRecipients() error = %v", err)
	}
	want := []string{"alice@example.com", "bob@example.com", "hidden@example.com", "ops@example.com"}
	if !slices.Equal(got, want) {
		t.Fatalf("resolveRecipients() = %v, want %v", got, want)
	}
}

func TestResolveRecipientsMalformedMIMEUsesEnvFallback(t *testing.T) {
	t.Parallel()

	service := Service{
		EnvLookup: func(key string) string {
			if key == "MIMECRYPT_PGP_RECIPIENTS" {
				return "fallback@example.com"
			}
			return ""
		},
	}

	got, err := service.resolveRecipients([]byte("this is not a MIME header block"))
	if err != nil {
		t.Fatalf("resolveRecipients() error = %v", err)
	}
	want := []string{"fallback@example.com"}
	if !slices.Equal(got, want) {
		t.Fatalf("resolveRecipients() = %v, want %v", got, want)
	}
}

func TestResolveRecipientsMalformedMIMEWithoutEnvReturnsError(t *testing.T) {
	t.Parallel()

	service := Service{
		EnvLookup: func(string) string { return "" },
	}
	_, err := service.resolveRecipients([]byte("this is not a MIME header block"))
	if err == nil || !strings.Contains(err.Error(), "解析 MIME 头失败") {
		t.Fatalf("expected MIME parse error, got %v", err)
	}
}

func TestNormalizeCRLFRemovesBareLF(t *testing.T) {
	t.Parallel()

	got := normalizeCRLF([]byte("a\nb\r\nc\r\nd\n"))
	if string(got) != "a\r\nb\r\nc\r\nd" {
		t.Fatalf("normalizeCRLF() = %q, want %q", string(got), "a\r\nb\r\nc\r\nd")
	}
	assertNoBareLF(t, got)
}

func TestBuildPGPMIMEMessageInvalidInputReturnsError(t *testing.T) {
	t.Parallel()

	_, err := buildPGPMIMEMessage([]byte("not-a-mail"), []byte("cipher"))
	if err == nil {
		t.Fatalf("expected error for invalid MIME input")
	}
}

func TestBuildPGPMIMEMessageAddsDateIfMissing(t *testing.T) {
	t.Parallel()

	input := []byte(
		"From: sender@example.com\r\n" +
			"To: test@example.com\r\n" +
			"Subject: no date\r\n" +
			"\r\n" +
			"hello\r\n",
	)
	out, err := buildPGPMIMEMessage(input, []byte("-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----\n"))
	if err != nil {
		t.Fatalf("buildPGPMIMEMessage() error = %v", err)
	}

	msg, err := mail.ReadMessage(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("mail.ReadMessage() error = %v", err)
	}
	if strings.TrimSpace(msg.Header.Get("Date")) == "" {
		t.Fatalf("Date header should be auto-generated")
	}
}

func TestRunUsesBccForEncryptionButDoesNotEmitBccHeader(t *testing.T) {
	t.Parallel()

	encryptor := &fakeEncryptor{
		output: []byte("-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----\n"),
	}
	service := Service{Encryptor: encryptor}
	input := []byte(
		"From: sender@example.com\r\n" +
			"To: alice@example.com\r\n" +
			"Bcc: hidden@example.com\r\n" +
			"Subject: hi\r\n" +
			"\r\n" +
			"hello\r\n",
	)

	result, err := service.Run(input)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !slices.Equal(encryptor.gotRecipients, []string{"alice@example.com", "hidden@example.com"}) {
		t.Fatalf("unexpected recipients: %v", encryptor.gotRecipients)
	}
	if bytes.Contains(result.MIME, []byte("\r\nBcc:")) || bytes.HasPrefix(result.MIME, []byte("Bcc:")) {
		t.Fatalf("encrypted MIME must not expose Bcc header")
	}
}

func TestRunEncryptorErrorIsPropagated(t *testing.T) {
	t.Parallel()

	service := Service{
		Encryptor: &fakeEncryptor{err: errors.New("encrypt failed")},
		EnvLookup: func(key string) string {
			if key == "MIMECRYPT_PGP_RECIPIENTS" {
				return "ops@example.com"
			}
			return ""
		},
	}

	_, err := service.Run([]byte("From: sender@example.com\r\nSubject: test\r\n\r\nhello"))
	if err == nil || !strings.Contains(err.Error(), "encrypt failed") {
		t.Fatalf("expected encryptor error, got %v", err)
	}
}

func TestRunProducesRFC3156StructureForThunderbird(t *testing.T) {
	t.Parallel()

	encryptor := &fakeEncryptor{
		output: []byte("-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----\n"),
	}
	service := Service{Encryptor: encryptor}
	input := []byte(
		"From: sender@example.com\r\n" +
			"To: thunderbird@example.com\r\n" +
			"Subject: thunderbird compatibility\r\n" +
			"Date: Thu, 02 Jan 2026 10:00:00 +0000\r\n" +
			"Message-ID: <m1@example.com>\r\n" +
			"\r\n" +
			"hello\r\n",
	)

	result, err := service.Run(input)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Format != "pgp-mime" || !result.Encrypted || result.AlreadyEncrypted {
		t.Fatalf("unexpected result: %+v", result)
	}

	msg, parts := mustParseRFC3156Message(t, result.MIME)
	if msg.Header.Get("Subject") != "thunderbird compatibility" {
		t.Fatalf("subject lost after wrapping")
	}
	if got := msg.Header.Get(processedHeaderKey); got != processedHeaderValue {
		t.Fatalf("%s = %q, want %q", processedHeaderKey, got, processedHeaderValue)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 multipart parts, got %d", len(parts))
	}

	if parts[0].ContentType != "application/pgp-encrypted" {
		t.Fatalf("part1 content-type = %s, want application/pgp-encrypted", parts[0].ContentType)
	}
	if strings.TrimSpace(string(parts[0].Body)) != "Version: 1" {
		t.Fatalf("part1 body = %q, want Version: 1", string(parts[0].Body))
	}

	if parts[1].ContentType != "application/octet-stream" {
		t.Fatalf("part2 content-type = %s, want application/octet-stream", parts[1].ContentType)
	}
	if strings.ToLower(parts[1].Params["name"]) != "encrypted.pgp" {
		t.Fatalf("part2 name param = %q, want encrypted.pgp", parts[1].Params["name"])
	}
	if !bytes.Contains(parts[1].Body, []byte("-----BEGIN PGP MESSAGE-----")) {
		t.Fatalf("missing armored pgp payload")
	}

	assertNoBareLF(t, result.MIME)
}

func TestGPGEncryptorNoRecipients(t *testing.T) {
	t.Parallel()

	_, err := gpgEncryptor{binary: "gpg"}.Encrypt(context.Background(), []byte("data"), nil)
	if !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("expected ErrNoRecipients, got %v", err)
	}
}

func TestGPGEncryptorMissingBinary(t *testing.T) {
	t.Parallel()

	_, err := gpgEncryptor{binary: "/definitely/not/found/gpg"}.Encrypt(context.Background(), []byte("data"), []string{"a@example.com"})
	if err == nil || !strings.Contains(err.Error(), "执行 gpg 失败") {
		t.Fatalf("expected command execution error, got %v", err)
	}
}

func TestGPGEncryptorEmptyOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script helper is unix-only")
	}

	script := writeExecutable(t, "#!/bin/sh\nset -eu\ncat >/dev/null\n")
	_, err := gpgEncryptor{binary: script}.Encrypt(context.Background(), []byte("data"), []string{"a@example.com"})
	if err == nil || !strings.Contains(err.Error(), "gpg 输出为空") {
		t.Fatalf("expected empty output error, got %v", err)
	}
}

func TestGPGEncryptorInvokesBinaryWithExpectedFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script helper is unix-only")
	}

	argsFile := t.TempDir() + "/args.txt"
	script := writeExecutable(t, fmt.Sprintf(
		"#!/bin/sh\nset -eu\nprintf '%%s\\n' \"$@\" > %q\ncat >/dev/null\ncat <<'EOF'\n-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----\nEOF\n",
		argsFile,
	))

	enc := gpgEncryptor{binary: script}
	out, err := enc.Encrypt(context.Background(), []byte("hello"), []string{"alice@example.com", "bob@example.com"})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if !bytes.Contains(out, []byte("-----BEGIN PGP MESSAGE-----")) {
		t.Fatalf("unexpected output: %q", string(out))
	}

	rawArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("ReadFile(args) error = %v", err)
	}
	gotArgs := strings.Split(strings.TrimSpace(string(rawArgs)), "\n")
	wantArgs := []string{
		"--batch",
		"--yes",
		"--armor",
		"--trust-model",
		"always",
		"--encrypt",
		"--output",
		"-",
		"--recipient",
		"alice@example.com",
		"--recipient",
		"bob@example.com",
	}
	if !slices.Equal(gotArgs, wantArgs) {
		t.Fatalf("gpg args mismatch\ngot:  %v\nwant: %v", gotArgs, wantArgs)
	}
}

func TestDefaultGPGBinaryReadsEnv(t *testing.T) {
	t.Setenv("MIMECRYPT_GPG_BINARY", "")
	if got := defaultGPGBinary(); got != "gpg" {
		t.Fatalf("defaultGPGBinary() = %q, want gpg", got)
	}

	t.Setenv("MIMECRYPT_GPG_BINARY", "/tmp/custom-gpg")
	if got := defaultGPGBinary(); got != "/tmp/custom-gpg" {
		t.Fatalf("defaultGPGBinary() = %q, want /tmp/custom-gpg", got)
	}
}

func TestRunWithRealGPGRoundTripDecrypt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gpg integration test is unix-focused")
	}

	gpgPath, err := exec.LookPath("gpg")
	if err != nil {
		t.Skip("gpg not installed")
	}

	gnupgHome := t.TempDir()
	if err := os.Chmod(gnupgHome, 0o700); err != nil {
		t.Fatalf("chmod gnupghome: %v", err)
	}
	t.Setenv("GNUPGHOME", gnupgHome)

	keyParams := "" +
		"Key-Type: RSA\n" +
		"Key-Length: 2048\n" +
		"Subkey-Type: RSA\n" +
		"Subkey-Length: 2048\n" +
		"Name-Real: MimeCrypt Test\n" +
		"Name-Email: thunderbird@example.com\n" +
		"Expire-Date: 0\n" +
		"%no-protection\n" +
		"%commit\n"
	gen := exec.Command(gpgPath, "--batch", "--generate-key")
	gen.Stdin = strings.NewReader(keyParams)
	genOutput, err := gen.CombinedOutput()
	if err != nil {
		t.Fatalf("generate test key failed: %v\n%s", err, string(genOutput))
	}

	service := Service{
		Encryptor: gpgEncryptor{binary: gpgPath},
	}
	input := []byte(
		"From: sender@example.com\r\n" +
			"To: thunderbird@example.com\r\n" +
			"Subject: round trip\r\n" +
			"Date: Thu, 02 Jan 2026 10:00:00 +0000\r\n" +
			"\r\n" +
			"Hello Thunderbird!\r\n",
	)

	result, err := service.Run(input)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	_, parts := mustParseRFC3156Message(t, result.MIME)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}

	decryptCmd := exec.Command(gpgPath, "--batch", "--yes", "--decrypt")
	decryptCmd.Stdin = bytes.NewReader(parts[1].Body)
	var decrypted bytes.Buffer
	var stderr bytes.Buffer
	decryptCmd.Stdout = &decrypted
	decryptCmd.Stderr = &stderr
	if err := decryptCmd.Run(); err != nil {
		t.Fatalf("decrypt failed: %v, stderr=%s", err, strings.TrimSpace(stderr.String()))
	}

	if !bytes.Equal(decrypted.Bytes(), input) {
		t.Fatalf("decrypted payload mismatch\nwant: %q\ngot:  %q", string(input), decrypted.String())
	}
}

func TestRunWithEnvCustomGPGBinaryRoundTripDecrypt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gpg integration test is unix-focused")
	}

	gpgPath, err := exec.LookPath("gpg")
	if err != nil {
		t.Skip("gpg not installed")
	}

	gnupgHome := t.TempDir()
	if err := os.Chmod(gnupgHome, 0o700); err != nil {
		t.Fatalf("chmod gnupghome: %v", err)
	}
	t.Setenv("GNUPGHOME", gnupgHome)
	t.Setenv("MIMECRYPT_GPG_BINARY", gpgPath)

	keyParams := "" +
		"Key-Type: RSA\n" +
		"Key-Length: 2048\n" +
		"Subkey-Type: RSA\n" +
		"Subkey-Length: 2048\n" +
		"Name-Real: MimeCrypt Custom GPG Test\n" +
		"Name-Email: customgpg@example.com\n" +
		"Expire-Date: 0\n" +
		"%no-protection\n" +
		"%commit\n"
	gen := exec.Command(gpgPath, "--batch", "--generate-key")
	gen.Stdin = strings.NewReader(keyParams)
	genOutput, err := gen.CombinedOutput()
	if err != nil {
		t.Fatalf("generate test key failed: %v\n%s", err, string(genOutput))
	}

	// Service 使用默认 encryptor，通过 MIMECRYPT_GPG_BINARY 选择 gpg 路径。
	service := Service{}
	input := []byte(
		"From: sender@example.com\r\n" +
			"To: customgpg@example.com\r\n" +
			"Subject: custom gpg round trip\r\n" +
			"Date: Thu, 02 Jan 2026 10:00:00 +0000\r\n" +
			"\r\n" +
			"Hello Custom GPG!\r\n",
	)

	result, err := service.Run(input)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	_, parts := mustParseRFC3156Message(t, result.MIME)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}

	decryptCmd := exec.Command(gpgPath, "--batch", "--yes", "--decrypt")
	decryptCmd.Stdin = bytes.NewReader(parts[1].Body)
	var decrypted bytes.Buffer
	var stderr bytes.Buffer
	decryptCmd.Stdout = &decrypted
	decryptCmd.Stderr = &stderr
	if err := decryptCmd.Run(); err != nil {
		t.Fatalf("decrypt failed: %v, stderr=%s", err, strings.TrimSpace(stderr.String()))
	}

	if !bytes.Equal(decrypted.Bytes(), input) {
		t.Fatalf("decrypted payload mismatch\nwant: %q\ngot:  %q", string(input), decrypted.String())
	}
}

func mustParseRFC3156Message(t *testing.T, mimeBytes []byte) (*mail.Message, []parsedPart) {
	t.Helper()

	msg, err := mail.ReadMessage(bytes.NewReader(mimeBytes))
	if err != nil {
		t.Fatalf("mail.ReadMessage() error = %v", err)
	}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse top-level content-type failed: %v", err)
	}
	if strings.ToLower(mediaType) != "multipart/encrypted" {
		t.Fatalf("top-level content-type = %s, want multipart/encrypted", mediaType)
	}
	if strings.ToLower(params["protocol"]) != "application/pgp-encrypted" {
		t.Fatalf("protocol param = %q, want application/pgp-encrypted", params["protocol"])
	}
	boundary := params["boundary"]
	if strings.TrimSpace(boundary) == "" {
		t.Fatalf("missing boundary")
	}

	reader := multipart.NewReader(msg.Body, boundary)
	var parts []parsedPart
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read next part failed: %v", err)
		}
		contentType, partParams, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse part content-type failed: %v", err)
		}
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read part body failed: %v", err)
		}

		rawHeader := make(map[string][]string, len(part.Header))
		for k, values := range part.Header {
			rawHeader[k] = append([]string(nil), values...)
		}
		parts = append(parts, parsedPart{
			ContentType: strings.ToLower(contentType),
			Params:      partParams,
			Body:        body,
			RawHeader:   rawHeader,
		})
	}

	return msg, parts
}

func writeExecutable(t *testing.T, content string) string {
	t.Helper()

	path := t.TempDir() + "/mock-gpg.sh"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write script failed: %v", err)
	}
	return path
}

func assertNoBareLF(t *testing.T, data []byte) {
	t.Helper()

	for i, b := range data {
		if b == '\n' && (i == 0 || data[i-1] != '\r') {
			t.Fatalf("found bare LF at byte index %d", i)
		}
	}
}
