package provider

import (
	"io"
	"strings"
	"testing"
)

func TestWriteRequestOpenMIMEFromOpener(t *testing.T) {
	t.Parallel()

	req := WriteRequest{
		MIMEOpener: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("streamed")), nil
		},
	}

	reader, err := req.OpenMIME()
	if err != nil {
		t.Fatalf("OpenMIME() error = %v", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "streamed" {
		t.Fatalf("OpenMIME() = %q, want streamed", string(data))
	}
}

func TestWriteRequestReadMIMEFromOpener(t *testing.T) {
	t.Parallel()

	req := WriteRequest{
		MIMEOpener: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("streamed")), nil
		},
	}

	data, err := req.ReadMIME()
	if err != nil {
		t.Fatalf("ReadMIME() error = %v", err)
	}
	if string(data) != "streamed" {
		t.Fatalf("ReadMIME() = %q, want streamed", string(data))
	}
}
