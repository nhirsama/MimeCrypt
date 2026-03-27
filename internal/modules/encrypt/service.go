package encrypt

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"net/textproto"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

type Service struct {
	Encryptor MIMEEncryptor
	EnvLookup func(string) string
}

type Result struct {
	MIME             []byte
	Encrypted        bool
	AlreadyEncrypted bool
	Format           string
}

var ErrNoRecipients = errors.New("未找到可用的加密收件人，请设置 MIMECRYPT_PGP_RECIPIENTS 或在邮件头中提供 To/Cc/Bcc")
var ErrAlreadyEncrypted = errors.New("邮件已加密，拒绝重复加密")

type AlreadyEncryptedError struct {
	Format string
}

func (e AlreadyEncryptedError) Error() string {
	if strings.TrimSpace(e.Format) == "" {
		return ErrAlreadyEncrypted.Error()
	}
	return fmt.Sprintf("%s: %s", ErrAlreadyEncrypted.Error(), e.Format)
}

func (e AlreadyEncryptedError) Is(target error) bool {
	return target == ErrAlreadyEncrypted
}

type MIMEEncryptor interface {
	Encrypt(mimeBytes []byte, recipients []string) ([]byte, error)
}

type gpgEncryptor struct {
	binary string
}

// Run 对邮件内容进行加密；已加密邮件会直接返回错误，防止重复加密。
func (s *Service) Run(mimeBytes []byte) (Result, error) {
	format, encrypted := detectFormat(mimeBytes)
	if encrypted {
		return Result{}, AlreadyEncryptedError{Format: format}
	}

	recipients, err := s.resolveRecipients(mimeBytes)
	if err != nil {
		return Result{}, err
	}
	if len(recipients) == 0 {
		return Result{}, ErrNoRecipients
	}

	armored, err := s.encryptor().Encrypt(mimeBytes, recipients)
	if err != nil {
		return Result{}, err
	}

	encryptedMIME, err := buildPGPMIMEMessage(mimeBytes, armored)
	if err != nil {
		return Result{}, err
	}

	return Result{
		MIME:             encryptedMIME,
		Encrypted:        true,
		AlreadyEncrypted: false,
		Format:           "pgp-mime",
	}, nil
}

func detectFormat(mimeBytes []byte) (string, bool) {
	lowerAll := strings.ToLower(string(mimeBytes))
	headerBytes := mimeBytes
	if idx := bytes.Index(mimeBytes, []byte("\r\n\r\n")); idx >= 0 {
		headerBytes = mimeBytes[:idx]
	} else if idx := bytes.Index(mimeBytes, []byte("\n\n")); idx >= 0 {
		headerBytes = mimeBytes[:idx]
	}

	lowerHeader := strings.ToLower(string(headerBytes))
	if strings.Contains(lowerHeader, "content-type: multipart/encrypted") &&
		strings.Contains(lowerHeader, "application/pgp-encrypted") {
		return "pgp-mime", true
	}
	if strings.Contains(lowerAll, "-----begin pgp message-----") {
		return "inline-pgp", true
	}
	if isSMIMEEncrypted(lowerHeader) {
		return "smime-enveloped", true
	}

	return "plain", false
}

func isSMIMEEncrypted(lowerHeader string) bool {
	if !(strings.Contains(lowerHeader, "content-type: application/pkcs7-mime") ||
		strings.Contains(lowerHeader, "content-type: application/x-pkcs7-mime")) {
		return false
	}

	if strings.Contains(lowerHeader, "smime-type=enveloped-data") {
		return true
	}
	if strings.Contains(lowerHeader, "name=\"smime.p7m\"") {
		return true
	}
	if strings.Contains(lowerHeader, "filename=\"smime.p7m\"") {
		return true
	}

	return false
}

func (s *Service) encryptor() MIMEEncryptor {
	if s != nil && s.Encryptor != nil {
		return s.Encryptor
	}
	return gpgEncryptor{binary: defaultGPGBinary()}
}

func defaultGPGBinary() string {
	if value := strings.TrimSpace(os.Getenv("MIMECRYPT_GPG_BINARY")); value != "" {
		return value
	}
	return "gpg"
}

func (s *Service) resolveRecipients(mimeBytes []byte) ([]string, error) {
	recipients := collectRecipientsFromEnv(s.getenv("MIMECRYPT_PGP_RECIPIENTS"))

	message, err := mail.ReadMessage(bytes.NewReader(mimeBytes))
	if err != nil {
		if len(recipients) == 0 {
			return nil, fmt.Errorf("解析 MIME 头失败: %w", err)
		}
		return dedupeRecipients(recipients), nil
	}

	headers := []string{"To", "Cc", "Bcc"}
	for _, key := range headers {
		values := headerValues(message.Header, key)
		for _, raw := range values {
			recipients = append(recipients, parseAddressList(raw)...)
		}
	}

	return dedupeRecipients(recipients), nil
}

func (s *Service) getenv(key string) string {
	if s != nil && s.EnvLookup != nil {
		return s.EnvLookup(key)
	}

	return os.Getenv(key)
}

func collectRecipientsFromEnv(value string) []string {
	return parseAddressList(strings.NewReplacer(";", ",", "\n", ",").Replace(value))
}

func parseAddressList(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	addresses, err := mail.ParseAddressList(trimmed)
	if err == nil {
		result := make([]string, 0, len(addresses))
		for _, addr := range addresses {
			email := strings.TrimSpace(strings.ToLower(addr.Address))
			if email != "" {
				result = append(result, email)
			}
		}
		return result
	}

	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		email := strings.TrimSpace(strings.ToLower(part))
		if email != "" {
			result = append(result, email)
		}
	}

	return result
}

func dedupeRecipients(recipients []string) []string {
	seen := make(map[string]struct{}, len(recipients))
	result := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		key := strings.TrimSpace(strings.ToLower(recipient))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}

	slices.Sort(result)
	return result
}

func buildPGPMIMEMessage(originalMIME, armored []byte) ([]byte, error) {
	message, err := mail.ReadMessage(bytes.NewReader(originalMIME))
	if err != nil {
		return nil, fmt.Errorf("解析原始 MIME 失败: %w", err)
	}

	boundary, err := newBoundary()
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	writeHeaders(&out, message.Header, boundary)
	out.WriteString("\r\n")

	out.WriteString("--")
	out.WriteString(boundary)
	out.WriteString("\r\n")
	out.WriteString("Content-Type: application/pgp-encrypted\r\n")
	out.WriteString("Content-Transfer-Encoding: 7bit\r\n")
	out.WriteString("\r\n")
	out.WriteString("Version: 1\r\n")
	out.WriteString("\r\n")

	out.WriteString("--")
	out.WriteString(boundary)
	out.WriteString("\r\n")
	out.WriteString("Content-Type: application/octet-stream; name=\"encrypted.asc\"\r\n")
	out.WriteString("Content-Disposition: inline; filename=\"encrypted.asc\"\r\n")
	out.WriteString("Content-Transfer-Encoding: 7bit\r\n")
	out.WriteString("\r\n")
	out.Write(normalizeCRLF(armored))
	out.WriteString("\r\n")

	out.WriteString("--")
	out.WriteString(boundary)
	out.WriteString("--\r\n")

	return out.Bytes(), nil
}

func writeHeaders(out *bytes.Buffer, header mail.Header, boundary string) {
	keys := []string{
		"From",
		"To",
		"Cc",
		"Subject",
		"Date",
		"Message-Id",
		"In-Reply-To",
		"References",
		"Reply-To",
	}
	for _, key := range keys {
		for _, value := range headerValues(header, key) {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			out.WriteString(key)
			out.WriteString(": ")
			out.WriteString(trimmed)
			out.WriteString("\r\n")
		}
	}

	if len(headerValues(header, "Date")) == 0 {
		out.WriteString("Date: ")
		out.WriteString(time.Now().UTC().Format(time.RFC1123Z))
		out.WriteString("\r\n")
	}
	out.WriteString("MIME-Version: 1.0\r\n")
	out.WriteString("Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=\"")
	out.WriteString(boundary)
	out.WriteString("\"\r\n")
}

func normalizeCRLF(input []byte) []byte {
	text := strings.ReplaceAll(string(input), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return []byte{}
	}
	return []byte(strings.ReplaceAll(text, "\n", "\r\n"))
}

func newBoundary() (string, error) {
	token := make([]byte, 8)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("生成 MIME boundary 失败: %w", err)
	}

	return "mimecrypt-" + hex.EncodeToString(token), nil
}

func headerValues(header mail.Header, key string) []string {
	if header == nil {
		return nil
	}
	canonical := textproto.CanonicalMIMEHeaderKey(key)
	return append([]string(nil), header[canonical]...)
}

func (g gpgEncryptor) Encrypt(mimeBytes []byte, recipients []string) ([]byte, error) {
	if len(recipients) == 0 {
		return nil, ErrNoRecipients
	}
	binary := strings.TrimSpace(g.binary)
	if binary == "" {
		binary = "gpg"
	}

	args := []string{"--batch", "--yes", "--armor", "--trust-model", "always", "--encrypt", "--output", "-"}
	for _, recipient := range recipients {
		args = append(args, "--recipient", recipient)
	}

	cmd := exec.Command(binary, args...)
	cmd.Stdin = bytes.NewReader(mimeBytes)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, fmt.Errorf("执行 gpg 失败: %w", err)
		}
		return nil, fmt.Errorf("执行 gpg 失败: %w: %s", err, msg)
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("gpg 输出为空")
	}

	return stdout.Bytes(), nil
}
