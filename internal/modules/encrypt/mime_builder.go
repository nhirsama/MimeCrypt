package encrypt

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

const (
	processedHeaderKey   = "X-MimeCrypt-Processed"
	processedHeaderValue = "yes"
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
	boundary        string
	pendingNewlines int
	prevCR          bool
	closed          bool
}

func newPGPMIMEMessageWriter(originalMIME []byte, out io.Writer, protectSubject bool) (*pgpMIMEMessageWriter, error) {
	message, err := mail.ReadMessage(bytes.NewReader(originalMIME))
	if err != nil {
		return nil, fmt.Errorf("解析原始 MIME 失败: %w", err)
	}
	return newPGPMIMEMessageWriterFromHeader(message.Header, out, protectSubject)
}

func newPGPMIMEMessageWriterFromHeader(header mail.Header, out io.Writer, protectSubject bool) (*pgpMIMEMessageWriter, error) {
	boundary, err := newBoundary()
	if err != nil {
		return nil, err
	}

	buffered := bufio.NewWriter(out)
	if err := writePGPMIMEPreamble(buffered, header, boundary, protectSubject); err != nil {
		return nil, err
	}

	return &pgpMIMEMessageWriter{
		writer:   buffered,
		boundary: boundary,
	}, nil
}

func writePGPMIMEPreamble(out io.Writer, header mail.Header, boundary string, protectSubject bool) error {
	if err := writeHeaders(out, header, boundary, protectSubject); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "This is an OpenPGP/MIME encrypted message (RFC 4880 and 3156)\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "--"+boundary+"\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "Content-Type: application/pgp-encrypted\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "Content-Description: PGP/MIME version identification\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "Content-Disposition: attachment\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "Content-Transfer-Encoding: 7bit\r\n\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "Version: 1\r\n\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "--"+boundary+"\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "Content-Type: application/octet-stream; name=\"encrypted.asc\"\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "Content-Description: OpenPGP encrypted message\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "Content-Disposition: inline; filename=\"encrypted.asc\"\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "Content-Transfer-Encoding: 7bit\r\n\r\n"); err != nil {
		return err
	}
	return nil
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
	w.pendingNewlines = 0

	if _, err := io.WriteString(w.writer, "\r\n--"+w.boundary+"--\r\n"); err != nil {
		return err
	}
	return w.writer.Flush()
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

func writeHeaders(out io.Writer, header mail.Header, boundary string, protectSubject bool) error {
	for _, key := range passThroughHeaderKeys {
		if protectSubject && strings.EqualFold(key, "Subject") {
			if hasNonEmptyHeaderValue(header, key) {
				if _, err := io.WriteString(out, "Subject: ...\r\n"); err != nil {
					return err
				}
			}
			continue
		}
		for _, value := range headerValues(header, key) {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			if _, err := io.WriteString(out, key+": "+trimmed+"\r\n"); err != nil {
				return err
			}
		}
	}

	if len(headerValues(header, "Date")) == 0 {
		if _, err := io.WriteString(out, "Date: "+time.Now().UTC().Format(time.RFC1123Z)+"\r\n"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(out, "MIME-Version: 1.0\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "Content-Transfer-Encoding: 7bit\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, processedHeaderKey+": "+processedHeaderValue+"\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, "Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=\""+boundary+"\"\r\n"); err != nil {
		return err
	}
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

func hasNonEmptyHeaderValue(header mail.Header, key string) bool {
	for _, value := range headerValues(header, key) {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}
