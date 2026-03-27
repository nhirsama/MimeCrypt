package encrypt

import (
	"bytes"
	"strings"
)

func detectFormat(mimeBytes []byte) (string, bool) {
	lowerAll := strings.ToLower(string(mimeBytes))
	lowerHeader := strings.ToLower(string(extractHeaderBlock(mimeBytes)))

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

func extractHeaderBlock(mimeBytes []byte) []byte {
	headerBytes := mimeBytes
	if idx := bytes.Index(mimeBytes, []byte("\r\n\r\n")); idx >= 0 {
		return headerBytes[:idx]
	}
	if idx := bytes.Index(mimeBytes, []byte("\n\n")); idx >= 0 {
		return headerBytes[:idx]
	}

	return headerBytes
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
