package encrypt

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	message "github.com/emersion/go-message"
)

const (
	processedHeaderKey   = "X-MimeCrypt-Processed"
	processedHeaderValue = "yes"
	boundaryMaxAttempts  = 8
)

var passThroughHeaderKeys = []string{
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

var boundaryGenerator = randomBoundary

func buildPGPMIMEMessage(originalMIME, armored []byte, protectSubject ...bool) ([]byte, error) {
	var out bytes.Buffer
	writer, err := newPGPMIMEMessageWriter(originalMIME, &out, len(protectSubject) > 0 && protectSubject[0])
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(armored); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type pgpMIMEMessageWriter struct {
	writer          *bufio.Writer
	partWriter      *message.Writer
	rootWriter      *message.Writer
	pendingNewlines int
	prevCR          bool
	closed          bool
}

func newPGPMIMEMessageWriter(originalMIME []byte, out io.Writer, protectSubject bool) (*pgpMIMEMessageWriter, error) {
	entity, err := message.Read(bytes.NewReader(originalMIME))
	if err != nil {
		return nil, fmt.Errorf("解析原始 MIME 失败: %w", err)
	}
	return newPGPMIMEMessageWriterFromHeaderWithSource(entity.Header, originalMIME, out, protectSubject)
}

func newPGPMIMEMessageWriterFromHeader(header message.Header, out io.Writer, protectSubject bool) (*pgpMIMEMessageWriter, error) {
	return newPGPMIMEMessageWriterFromHeaderWithSource(header, nil, out, protectSubject)
}

func newPGPMIMEMessageWriterFromHeaderWithSource(header message.Header, originalMIME []byte, out io.Writer, protectSubject bool) (*pgpMIMEMessageWriter, error) {
	boundary, err := newBoundary(originalMIME)
	if err != nil {
		return nil, err
	}

	outerHeader := buildPGPMIMEHeader(header, boundary, protectSubject)
	rootWriter, err := message.CreateWriter(out, outerHeader)
	if err != nil {
		return nil, fmt.Errorf("创建外层 MIME 失败: %w", err)
	}

	if _, err := io.WriteString(rootWriter, "This is an OpenPGP/MIME encrypted message (RFC 4880 and 3156)\r\n"); err != nil {
		_ = rootWriter.Close()
		return nil, err
	}

	versionHeader := message.Header{}
	versionHeader.SetContentType("application/pgp-encrypted", nil)
	versionHeader.Set("Content-Description", "PGP/MIME version identification")
	versionHeader.Set("Content-Disposition", "attachment")
	versionHeader.Set("Content-Transfer-Encoding", "7bit")

	versionWriter, err := rootWriter.CreatePart(versionHeader)
	if err != nil {
		_ = rootWriter.Close()
		return nil, fmt.Errorf("创建 PGP 版本 part 失败: %w", err)
	}
	if _, err := io.WriteString(versionWriter, "Version: 1\r\n"); err != nil {
		_ = versionWriter.Close()
		_ = rootWriter.Close()
		return nil, err
	}
	if err := versionWriter.Close(); err != nil {
		_ = rootWriter.Close()
		return nil, err
	}

	encryptedHeader := message.Header{}
	encryptedHeader.SetContentType("application/octet-stream", map[string]string{"name": "encrypted.asc"})
	encryptedHeader.Set("Content-Description", "OpenPGP encrypted message")
	encryptedHeader.Set("Content-Disposition", `inline; filename="encrypted.asc"`)
	encryptedHeader.Set("Content-Transfer-Encoding", "7bit")

	partWriter, err := rootWriter.CreatePart(encryptedHeader)
	if err != nil {
		_ = rootWriter.Close()
		return nil, fmt.Errorf("创建加密内容 part 失败: %w", err)
	}

	return &pgpMIMEMessageWriter{
		writer:     bufio.NewWriter(partWriter),
		partWriter: partWriter,
		rootWriter: rootWriter,
	}, nil
}

func buildPGPMIMEHeader(header message.Header, boundary string, protectSubject bool) message.Header {
	result := message.Header{}
	for _, key := range passThroughHeaderKeys {
		if protectSubject && strings.EqualFold(key, "Subject") {
			if hasNonEmptyHeaderValue(header, key) {
				result.Add("Subject", "...")
			}
			continue
		}
		for _, value := range headerValues(header, key) {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			result.Add(key, trimmed)
		}
	}

	if len(headerValues(header, "Date")) == 0 {
		result.Add("Date", time.Now().UTC().Format(time.RFC1123Z))
	}
	result.Set("MIME-Version", "1.0")
	result.Set("Content-Transfer-Encoding", "7bit")
	result.AddRaw([]byte(processedHeaderKey + ": " + processedHeaderValue + "\r\n"))
	result.AddRaw([]byte(fmt.Sprintf(
		"Content-Type: multipart/encrypted; protocol=%q;\r\n boundary=%q\r\n",
		"application/pgp-encrypted",
		boundary,
	)))
	return result
}

func (w *pgpMIMEMessageWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("PGP MIME writer 已关闭")
	}

	for _, b := range p {
		if w.prevCR {
			if b == '\n' {
				w.queueNewline()
				w.prevCR = false
				continue
			}
			w.queueNewline()
			w.prevCR = false
		}

		switch b {
		case '\r':
			w.prevCR = true
		case '\n':
			w.queueNewline()
		default:
			if err := w.flushPendingNewlines(); err != nil {
				return 0, err
			}
			if err := w.writer.WriteByte(b); err != nil {
				return 0, err
			}
		}
	}

	return len(p), nil
}

func (w *pgpMIMEMessageWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	if w.prevCR {
		w.queueNewline()
		w.prevCR = false
	}
	if err := w.flushPendingNewlines(); err != nil {
		return err
	}
	if err := w.writer.Flush(); err != nil {
		return err
	}
	if err := w.partWriter.Close(); err != nil {
		return err
	}
	return w.rootWriter.Close()
}

func (w *pgpMIMEMessageWriter) queueNewline() {
	w.pendingNewlines++
}

func (w *pgpMIMEMessageWriter) flushPendingNewlines() error {
	for i := 0; i < w.pendingNewlines; i++ {
		if _, err := io.WriteString(w.writer, "\r\n"); err != nil {
			return err
		}
	}
	w.pendingNewlines = 0
	return nil
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

func newBoundary(originalMIME []byte) (string, error) {
	for i := 0; i < boundaryMaxAttempts; i++ {
		boundary, err := boundaryGenerator()
		if err != nil {
			return "", err
		}
		if len(originalMIME) == 0 || !bytes.Contains(originalMIME, []byte(boundary)) {
			return boundary, nil
		}
	}
	return "", fmt.Errorf("生成 MIME boundary 失败: 与原始内容发生冲突")
}

func randomBoundary() (string, error) {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("生成 MIME boundary 失败: %w", err)
	}
	return "mimecrypt-" + hex.EncodeToString(token), nil
}

func headerValues(header message.Header, key string) []string {
	return append([]string(nil), header.Values(key)...)
}

func hasNonEmptyHeaderValue(header message.Header, key string) bool {
	for _, value := range headerValues(header, key) {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}
