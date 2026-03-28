package graph

import (
	"bytes"
	"encoding/base64"
	"io"
	"strings"
	"testing"
)

func TestNewBase64MIMEReader(t *testing.T) {
	t.Parallel()

	reader, err := newBase64MIMEReader(func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("hello")), nil
	})
	if err != nil {
		t.Fatalf("newBase64MIMEReader() error = %v", err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Fatalf("body = %q", string(body))
	}
}

func TestBuildCreateItemEnvelopeReader(t *testing.T) {
	t.Parallel()

	reader, err := buildCreateItemEnvelopeReader("folder-1", func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("hello")), nil
	})
	if err != nil {
		t.Fatalf("buildCreateItemEnvelopeReader() error = %v", err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Contains(body, []byte(`FolderId Id="folder-1"`)) {
		t.Fatalf("missing folder id in envelope: %s", string(body))
	}
	if !bytes.Contains(body, []byte(base64.StdEncoding.EncodeToString([]byte("hello")))) {
		t.Fatalf("missing base64 MIME payload in envelope")
	}
	if bytes.Contains(body, []byte(ewsBase64Placeholder)) {
		t.Fatalf("placeholder leaked into final envelope")
	}
}

func TestNewBase64MIMEReaderRejectsNilOpener(t *testing.T) {
	t.Parallel()

	_, err := newBase64MIMEReader(nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}
