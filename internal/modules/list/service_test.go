package list

import (
	"context"
	"errors"
	"testing"
	"time"

	"mimecrypt/internal/provider"
)

type fakeReader struct {
	folder   string
	skip     int
	limit    int
	messages []provider.Message
	err      error
}

func (f *fakeReader) LatestMessagesInFolder(_ context.Context, folder string, skip, limit int) ([]provider.Message, error) {
	f.folder = folder
	f.skip = skip
	f.limit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.messages, nil
}

func TestRunFetchesLatestMessageSlice(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		messages: []provider.Message{
			{ID: "m2", Subject: "two", ReceivedDateTime: time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)},
			{ID: "m3", Subject: "three", ReceivedDateTime: time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC)},
		},
	}
	service := Service{Client: reader}

	result, err := service.Run(context.Background(), Request{
		Folder: "inbox",
		Start:  1,
		End:    3,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if reader.folder != "inbox" || reader.skip != 1 || reader.limit != 2 {
		t.Fatalf("unexpected reader call: folder=%q skip=%d limit=%d", reader.folder, reader.skip, reader.limit)
	}
	if len(result.Messages) != 2 || result.Messages[0].ID != "m2" || result.Messages[1].ID != "m3" {
		t.Fatalf("unexpected result messages: %+v", result.Messages)
	}
}

func TestRunValidatesRequest(t *testing.T) {
	t.Parallel()

	service := Service{Client: &fakeReader{}}
	cases := []struct {
		name    string
		req     Request
		wantErr string
	}{
		{
			name:    "missing folder",
			req:     Request{Start: 0, End: 1},
			wantErr: "folder 不能为空",
		},
		{
			name:    "negative start",
			req:     Request{Folder: "inbox", Start: -1, End: 1},
			wantErr: "start 不能小于 0",
		},
		{
			name:    "end not greater than start",
			req:     Request{Folder: "inbox", Start: 2, End: 2},
			wantErr: "end 必须大于 start",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := service.Run(context.Background(), tc.req)
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("Run() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestRunWrapsReaderError(t *testing.T) {
	t.Parallel()

	service := Service{
		Client: &fakeReader{err: errors.New("boom")},
	}

	_, err := service.Run(context.Background(), Request{
		Folder: "inbox",
		Start:  0,
		End:    2,
	})
	if err == nil || err.Error() != "获取最新邮件列表失败: boom" {
		t.Fatalf("Run() error = %v", err)
	}
}
