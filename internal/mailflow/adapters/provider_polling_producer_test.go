package adapters

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/provider"
)

type fakeReader struct {
	messages    []provider.Message
	delta       string
	deltaCalls  int
	fetchBodies map[string]string
}

func (r *fakeReader) Me(context.Context) (provider.User, error) { return provider.User{}, nil }
func (r *fakeReader) Message(context.Context, string) (provider.Message, error) {
	return provider.Message{}, nil
}
func (r *fakeReader) FetchMIME(_ context.Context, messageID string) (io.ReadCloser, error) {
	body, ok := r.fetchBodies[messageID]
	if !ok {
		body = ""
	}
	return io.NopCloser(strings.NewReader(body)), nil
}
func (r *fakeReader) DeltaCreatedMessages(context.Context, string, string) ([]provider.Message, string, error) {
	r.deltaCalls++
	return append([]provider.Message(nil), r.messages...), r.delta, nil
}
func (r *fakeReader) FirstMessageInFolder(context.Context, string) (provider.Message, bool, error) {
	return provider.Message{}, false, nil
}
func (r *fakeReader) LatestMessagesInFolder(context.Context, string, int, int) ([]provider.Message, error) {
	return nil, nil
}

type fakeDeleter struct {
	deleted []provider.MessageRef
	err     error
}

func (d *fakeDeleter) DeleteMessage(_ context.Context, source provider.MessageRef) error {
	if d.err != nil {
		return d.err
	}
	d.deleted = append(d.deleted, source)
	return nil
}

func TestPollingProducerSkipsBootstrapMessagesByDefault(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		messages: []provider.Message{{ID: "m1"}, {ID: "m2"}},
		delta:    "delta-1",
	}
	producer := &PollingProducer{
		Name:      "office_inbox",
		Driver:    "graph",
		Folder:    "inbox",
		StatePath: t.TempDir() + "/producer.json",
		Reader:    reader,
	}

	_, err := producer.Next(context.Background())
	if !errors.Is(err, mailflow.ErrNoMessages) {
		t.Fatalf("Next() error = %v, want ErrNoMessages", err)
	}
	if reader.deltaCalls != 1 {
		t.Fatalf("deltaCalls = %d, want 1", reader.deltaCalls)
	}
}

func TestPollingProducerReturnsEnvelopeAndAcknowledgesPendingMessage(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		messages: []provider.Message{
			{ID: "m1", InternetMessageID: "<m1@example.com>", ParentFolderID: "INBOX", ReceivedDateTime: time.Date(2026, 3, 29, 2, 3, 4, 0, time.UTC)},
			{ID: "m2", InternetMessageID: "<m2@example.com>", ParentFolderID: "INBOX", ReceivedDateTime: time.Date(2026, 3, 29, 2, 4, 4, 0, time.UTC)},
		},
		delta:       "delta-2",
		fetchBodies: map[string]string{"m1": "mime-1", "m2": "mime-2"},
	}
	producer := &PollingProducer{
		Name:            "office_inbox",
		Driver:          "graph",
		Folder:          "INBOX",
		StatePath:       t.TempDir() + "/producer.json",
		IncludeExisting: true,
		Store: mailflow.StoreRef{
			Driver:  "graph",
			Account: "user@example.com",
			Mailbox: "INBOX",
		},
		Reader: reader,
	}

	first, err := producer.Next(context.Background())
	if err != nil {
		t.Fatalf("first Next() error = %v", err)
	}
	if first.Trace.TransactionKey != "office_inbox:m1" {
		t.Fatalf("TransactionKey = %q, want office_inbox:m1", first.Trace.TransactionKey)
	}
	mime, err := first.MIME()
	if err != nil {
		t.Fatalf("MIME() error = %v", err)
	}
	data, err := io.ReadAll(mime)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	_ = mime.Close()
	if string(data) != "mime-1" {
		t.Fatalf("mime = %q, want mime-1", string(data))
	}
	if err := first.Source.Acknowledge(context.Background()); err != nil {
		t.Fatalf("Acknowledge() error = %v", err)
	}

	second, err := producer.Next(context.Background())
	if err != nil {
		t.Fatalf("second Next() error = %v", err)
	}
	if second.Trace.SourceMessageID != "m2" {
		t.Fatalf("SourceMessageID = %q, want m2", second.Trace.SourceMessageID)
	}
	if reader.deltaCalls != 1 {
		t.Fatalf("deltaCalls = %d, want 1", reader.deltaCalls)
	}
}

func TestPollingProducerSourceDeleteUsesProviderDeleter(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		messages: []provider.Message{{ID: "m1", ParentFolderID: "INBOX"}},
		delta:    "delta-3",
	}
	deleter := &fakeDeleter{}
	producer := &PollingProducer{
		Name:            "office_inbox",
		Driver:          "graph",
		Folder:          "INBOX",
		StatePath:       t.TempDir() + "/producer.json",
		IncludeExisting: true,
		Reader:          reader,
		Deleter:         deleter,
	}

	envelope, err := producer.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	deletable, ok := envelope.Source.(mailflow.DeletableSource)
	if !ok {
		t.Fatalf("source does not implement DeletableSource")
	}
	if err := deletable.Delete(context.Background()); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(deleter.deleted) != 1 || deleter.deleted[0].ID != "m1" {
		t.Fatalf("unexpected deleted refs: %+v", deleter.deleted)
	}
}
