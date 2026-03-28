package encrypt

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
	message, err := mail.ReadMessage(bytes.NewReader(originalMIME))
	if err != nil {
		return nil, fmt.Errorf("解析原始 MIME 失败: %w", err)
	}
	enableProtectSubject := len(protectSubject) > 0 && protectSubject[0]

	boundary, err := newBoundary()
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	writeHeaders(&out, message.Header, boundary, enableProtectSubject)
	out.WriteString("\r\n")
	out.WriteString("This is an OpenPGP/MIME encrypted message (RFC 4880 and 3156)\r\n")

	out.WriteString("--")
	out.WriteString(boundary)
	out.WriteString("\r\n")
	out.WriteString("Content-Type: application/pgp-encrypted\r\n")
	out.WriteString("Content-Description: PGP/MIME version identification\r\n")
	out.WriteString("Content-Disposition: attachment\r\n")
	out.WriteString("Content-Transfer-Encoding: 7bit\r\n")
	out.WriteString("\r\n")
	out.WriteString("Version: 1\r\n")
	out.WriteString("\r\n")

	out.WriteString("--")
	out.WriteString(boundary)
	out.WriteString("\r\n")
	out.WriteString("Content-Type: application/octet-stream; name=\"encrypted.asc\"\r\n")
	out.WriteString("Content-Description: OpenPGP encrypted message\r\n")
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

func writeHeaders(out *bytes.Buffer, header mail.Header, boundary string, protectSubject bool) {
	for _, key := range passThroughHeaderKeys {
		if protectSubject && strings.EqualFold(key, "Subject") {
			if hasNonEmptyHeaderValue(header, key) {
				out.WriteString("Subject: ...\r\n")
			}
			continue
		}
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
	out.WriteString("Content-Transfer-Encoding: 7bit\r\n")
	out.WriteString(processedHeaderKey)
	out.WriteString(": ")
	out.WriteString(processedHeaderValue)
	out.WriteString("\r\n")
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

func hasNonEmptyHeaderValue(header mail.Header, key string) bool {
	for _, value := range headerValues(header, key) {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}
