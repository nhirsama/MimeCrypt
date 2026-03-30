package encrypt

import (
	"bytes"
	"io"
	"strings"

	message "github.com/emersion/go-message"
)

func detectFormat(mimeBytes []byte) (string, bool) {
	if entity, err := message.Read(bytes.NewReader(mimeBytes)); err == nil {
		switch {
		case isPGPMIMEHeader(entity.Header):
			return "pgp-mime", true
		case isSMIMEEncryptedHeader(entity.Header):
			return "smime-enveloped", true
		}
	} else {
		lowerHeader := strings.ToLower(string(extractHeaderBlock(mimeBytes)))
		if isPGPMIMEHeaderText(lowerHeader) {
			return "pgp-mime", true
		}
		if isSMIMEEncrypted(lowerHeader) {
			return "smime-enveloped", true
		}
	}

	if bytes.Contains(bytes.ToLower(mimeBytes), []byte("-----begin pgp message-----")) {
		return "inline-pgp", true
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

	entity, err := message.Read(reader)
	if err != nil {
		return "", false, err
	}

	switch {
	case isPGPMIMEHeader(entity.Header):
		return "pgp-mime", true, nil
	case isSMIMEEncryptedHeader(entity.Header):
		return "smime-enveloped", true, nil
	}

	found, err := streamContainsFold(entity.Body, []byte("-----BEGIN PGP MESSAGE-----"))
	if err != nil {
		return "", false, err
	}
	if found {
		return "inline-pgp", true, nil
	}
	return "plain", false, nil
}

func extractHeaderBlock(mimeBytes []byte) []byte {
	if idx := bytes.Index(mimeBytes, []byte("\r\n\r\n")); idx >= 0 {
		return mimeBytes[:idx]
	}
	if idx := bytes.Index(mimeBytes, []byte("\n\n")); idx >= 0 {
		return mimeBytes[:idx]
	}
	return mimeBytes
}

func headerToBytes(header message.Header) []byte {
	var out bytes.Buffer
	fields := header.Fields()
	for fields.Next() {
		out.WriteString(fields.Key())
		out.WriteString(": ")
		out.WriteString(fields.Value())
		out.WriteString("\r\n")
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

func isPGPMIMEHeader(header message.Header) bool {
	mediaType, params, _ := header.ContentType()
	return strings.EqualFold(strings.TrimSpace(mediaType), "multipart/encrypted") &&
		strings.EqualFold(strings.TrimSpace(params["protocol"]), "application/pgp-encrypted")
}

func isPGPMIMEHeaderText(lowerHeader string) bool {
	return strings.Contains(lowerHeader, "content-type: multipart/encrypted") &&
		strings.Contains(lowerHeader, "application/pgp-encrypted")
}

func isSMIMEEncryptedHeader(header message.Header) bool {
	mediaType, params, _ := header.ContentType()
	lowerType := strings.ToLower(strings.TrimSpace(mediaType))
	if lowerType != "application/pkcs7-mime" && lowerType != "application/x-pkcs7-mime" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(params["smime-type"]), "enveloped-data") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(params["name"]), "smime.p7m") {
		return true
	}

	_, dispParams, _ := header.ContentDisposition()
	return strings.EqualFold(strings.TrimSpace(dispParams["filename"]), "smime.p7m")
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
