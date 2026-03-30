package adapters

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/modules/writeback"
	"mimecrypt/internal/provider"
)

type fakeProviderWriter struct {
	req    provider.WriteRequest
	result provider.WriteResult
	err    error
}

func (f *fakeProviderWriter) WriteMessage(_ context.Context, req provider.WriteRequest) (provider.WriteResult, error) {
	f.req = req
	if f.err != nil {
		return provider.WriteResult{}, f.err
	}
	return f.result, nil
}

func TestWritebackConsumerForwardsWithoutDeleteSource(t *testing.T) {
	t.Parallel()

	writer := &fakeProviderWriter{result: provider.WriteResult{Verified: true}}
	consumer := &WritebackConsumer{
		Service: &writeback.Service{Writer: writer},
		Verify:  true,
		Store: mailflow.StoreRef{
			Driver:  "imap",
			Account: "archive@example.com",
			Mailbox: "Encrypted",
		},
	}

	result, err := consumer.Consume(context.Background(), mailflow.ConsumeRequest{
		Trace: mailflow.MailTrace{
			TransactionKey:    "tx-consume",
			SourceMessageID:   "m1",
			SourceFolderID:    "INBOX",
			InternetMessageID: "<m1@example.com>",
			ReceivedAt:        time.Date(2026, 3, 29, 1, 2, 3, 0, time.UTC),
		},
		Target: mailflow.DeliveryTarget{
			Name:     "archive-main",
			Consumer: "archive",
		},
		Artifact: mailflow.MailArtifact{
			Name: "primary",
			MIME: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("encrypted")), nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if !result.Verified {
		t.Fatalf("Verified = false, want true")
	}
	if writer.req.Source.ID != "m1" || writer.req.Source.FolderID != "INBOX" {
		t.Fatalf("unexpected source ref: %+v", writer.req.Source)
	}
	if !writer.req.Verify {
		t.Fatalf("Verify = false, want true")
	}
}

func TestWritebackConsumerReturnsServiceError(t *testing.T) {
	t.Parallel()

	consumer := &WritebackConsumer{
		Service: &writeback.Service{Writer: &fakeProviderWriter{err: errors.New("write failed")}},
	}

	_, err := consumer.Consume(context.Background(), mailflow.ConsumeRequest{
		Trace: mailflow.MailTrace{
			TransactionKey: "tx-consume-err",
		},
		Target: mailflow.DeliveryTarget{
			Name:     "archive-main",
			Consumer: "archive",
		},
		Artifact: mailflow.MailArtifact{
			Name: "primary",
			MIME: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("encrypted")), nil
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("Consume() error = %v, want write failed", err)
	}
}
