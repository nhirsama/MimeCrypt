package encrypt

import (
	"bufio"
	"bytes"
	"io"
	"net/mail"
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

type mimeOpenFunc func() (io.ReadCloser, error)

func detectFormatFromOpener(open mimeOpenFunc) (string, bool, error) {
	if open == nil {
		return "plain", false, nil
	}
	reader, err := open()
	if err != nil {
		return "", false, err
	}
	defer reader.Close()

	message, err := mail.ReadMessage(bufio.NewReader(reader))
	if err != nil {
		return "", false, err
	}

	lowerHeader := strings.ToLower(string(headerToBytes(message.Header)))
	if strings.Contains(lowerHeader, "content-type: multipart/encrypted") &&
		strings.Contains(lowerHeader, "application/pgp-encrypted") {
		return "pgp-mime", true, nil
	}
	if isSMIMEEncrypted(lowerHeader) {
		return "smime-enveloped", true, nil
	}

	found, err := streamContainsFold(message.Body, []byte("-----BEGIN PGP MESSAGE-----"))
	if err != nil {
		return "", false, err
	}
	if found {
		return "inline-pgp", true, nil
	}

	return "plain", false, nil
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

func headerToBytes(header mail.Header) []byte {
	if header == nil {
		return nil
	}
	var out bytes.Buffer
	for key, values := range header {
		for _, value := range values {
			out.WriteString(key)
			out.WriteString(": ")
			out.WriteString(value)
			out.WriteString("\r\n")
		}
	}
	return out.Bytes()
}

func streamContainsFold(r io.Reader, needle []byte) (bool, error) {
	needle = bytes.ToLower(needle)
	if len(needle) == 0 {
		return true, nil
	}
	buffer := make([]byte, 32*1024)
	carry := make([]byte, 0, len(needle)-1)

	for {
		n, err := r.Read(buffer)
		if n > 0 {
			chunk := append(append([]byte(nil), carry...), buffer[:n]...)
			lowerChunk := bytes.ToLower(chunk)
			if bytes.Contains(lowerChunk, needle) {
				return true, nil
			}
			if len(chunk) >= len(needle)-1 {
				carry = append(carry[:0], chunk[len(chunk)-len(needle)+1:]...)
			} else {
				carry = append(carry[:0], chunk...)
			}
		}
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
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
