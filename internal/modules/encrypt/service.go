package encrypt

import (
	"bytes"
	"strings"
)

type Service struct{}

type Result struct {
	MIME             []byte
	Encrypted        bool
	AlreadyEncrypted bool
	Format           string
}

// Run 对邮件内容进行加密判定；已加密邮件直接透传。
func (s *Service) Run(mimeBytes []byte) (Result, error) {
	format, encrypted := detectFormat(mimeBytes)

	return Result{
		MIME:             mimeBytes,
		Encrypted:        encrypted,
		AlreadyEncrypted: encrypted,
		Format:           format,
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

	return "plain", false
}
