package download

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/provider"
)

func TestFetchReturnsPayloadAndClosesStream(t *testing.T) {
	t.Parallel()

	stream := &trackingReadCloser{reader: strings.NewReader("mime-body")}
	reader := &fakeReader{
		message: provider.Message{ID: "m1", Subject: "subject"},
		stream:  stream,
	}
	service := Service{Client: reader}

	payload, err := service.Fetch(context.Background(), "m1")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if reader.messageID != "m1" || reader.mimeID != "m1" {
		t.Fatalf("unexpected IDs: message=%q mime=%q", reader.messageID, reader.mimeID)
	}
	if payload.Message.ID != "m1" || string(payload.MIME) != "mime-body" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if !stream.closed {
		t.Fatalf("expected MIME stream to be closed")
	}
}

func TestFetchWrapsReaderErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		reader  *fakeReader
		wantErr string
	}{
		{
			name: "message error",
			reader: &fakeReader{
				messageErr: errors.New("message boom"),
			},
			wantErr: "获取邮件元数据失败",
		},
		{
			name: "fetch mime error",
			reader: &fakeReader{
				message:  provider.Message{ID: "m1"},
				fetchErr: errors.New("fetch boom"),
			},
			wantErr: "获取邮件 MIME 失败",
		},
		{
			name: "read mime error",
			reader: &fakeReader{
				message: provider.Message{ID: "m1"},
				stream:  &trackingReadCloser{readErr: errors.New("read boom")},
			},
			wantErr: "读取邮件 MIME 失败",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := Service{Client: tc.reader}

			_, err := service.Fetch(context.Background(), "m1")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Fetch() error = %v, want substring %q", err, tc.wantErr)
			}
			if tc.reader.stream != nil && !tc.reader.stream.closed {
				t.Fatalf("expected stream to be closed on error")
			}
		})
	}
}

func TestSaveWritesFetchedMIME(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		message: provider.Message{
			ID:               "msg/1",
			ReceivedDateTime: time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC),
		},
		stream: &trackingReadCloser{reader: strings.NewReader("mime-body")},
	}
	service := Service{Client: reader}
	outputDir := filepath.Join(t.TempDir(), "out")

	result, err := service.Save(context.Background(), "msg/1", outputDir)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if result.Message.ID != "msg/1" {
		t.Fatalf("result.Message.ID = %q, want msg/1", result.Message.ID)
	}
	if result.Bytes != int64(len("mime-body")) {
		t.Fatalf("result.Bytes = %d, want %d", result.Bytes, len("mime-body"))
	}

	got, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "mime-body" {
		t.Fatalf("saved MIME = %q, want mime-body", got)
	}
}

func TestSavePayloadWrapsMimefileErrors(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(outputDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	service := Service{}
	_, err := service.SavePayload(Payload{
		Message: provider.Message{ID: "m1"},
		MIME:    []byte("mime"),
	}, outputDir)
	if err == nil || !strings.Contains(err.Error(), "保存邮件 MIME 失败") {
		t.Fatalf("SavePayload() error = %v, want wrapped mimefile error", err)
	}
}

type fakeReader struct {
	message    provider.Message
	messageErr error
	stream     *trackingReadCloser
	fetchErr   error
	messageID  string
	mimeID     string
}

func (f *fakeReader) Me(context.Context) (provider.User, error) {
	return provider.User{}, nil
}

func (f *fakeReader) Message(_ context.Context, messageID string) (provider.Message, error) {
	f.messageID = messageID
	if f.messageErr != nil {
		return provider.Message{}, f.messageErr
	}
	return f.message, nil
}

func (f *fakeReader) FetchMIME(_ context.Context, messageID string) (io.ReadCloser, error) {
	f.mimeID = messageID
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	if f.stream == nil {
		f.stream = &trackingReadCloser{reader: strings.NewReader("")}
	}
	return f.stream, nil
}

func (f *fakeReader) DeltaCreatedMessages(context.Context, string, string) ([]provider.Message, string, error) {
	return nil, "", nil
}

func (f *fakeReader) FirstMessageInFolder(context.Context, string) (provider.Message, bool, error) {
	return provider.Message{}, false, nil
}

type trackingReadCloser struct {
	reader  io.Reader
	readErr error
	closed  bool
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	return r.reader.Read(p)
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}
