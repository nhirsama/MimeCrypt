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

func TestReaderEnvelopeBuilderEnvelopeForMessage(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		fetchBodies: map[string]string{"m1": "mime-1"},
	}
	builder := &ReaderEnvelopeBuilder{
		Name:   "default",
		Driver: "graph",
		Folder: "INBOX",
		Store: mailflow.StoreRef{
			Driver:  "graph",
			Account: "user@example.com",
			Mailbox: "INBOX",
		},
		Reader: reader,
	}

	envelope, err := builder.EnvelopeForMessage(context.Background(), provider.Message{
		ID:                "m1",
		InternetMessageID: "<m1@example.com>",
		ReceivedDateTime:  time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("EnvelopeForMessage() error = %v", err)
	}
	if envelope.Trace.TransactionKey != "default:m1" {
		t.Fatalf("TransactionKey = %q, want default:m1", envelope.Trace.TransactionKey)
	}
	if envelope.Trace.SourceFolderID != "INBOX" {
		t.Fatalf("SourceFolderID = %q, want INBOX", envelope.Trace.SourceFolderID)
	}
	readerBody, err := envelope.MIME()
	if err != nil {
		t.Fatalf("MIME() error = %v", err)
	}
	body, err := io.ReadAll(readerBody)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	_ = readerBody.Close()
	if string(body) != "mime-1" {
		t.Fatalf("body = %q, want mime-1", string(body))
	}
}

func TestReaderEnvelopeBuilderEnvelopeForIDFetchesMessage(t *testing.T) {
	t.Parallel()

	reader := &readerEnvelopeReader{
		message: provider.Message{
			ID: "m2",
		},
		body: "mime-2",
	}
	builder := &ReaderEnvelopeBuilder{
		Name:   "default",
		Driver: "imap",
		Folder: "Archive",
		Reader: reader,
	}

	envelope, err := builder.EnvelopeForID(context.Background(), "m2", "Inbox")
	if err != nil {
		t.Fatalf("EnvelopeForID() error = %v", err)
	}
	if envelope.Trace.SourceFolderID != "Inbox" {
		t.Fatalf("SourceFolderID = %q, want Inbox", envelope.Trace.SourceFolderID)
	}
}

func TestReaderEnvelopeBuilderDeleteUsesDeleter(t *testing.T) {
	t.Parallel()

	deleter := &fakeDeleter{}
	builder := &ReaderEnvelopeBuilder{
		Name:    "default",
		Driver:  "imap",
		Folder:  "INBOX",
		Reader:  &readerEnvelopeReader{message: provider.Message{ID: "m3"}, body: "mime-3"},
		Deleter: deleter,
	}

	envelope, err := builder.EnvelopeForID(context.Background(), "m3", "")
	if err != nil {
		t.Fatalf("EnvelopeForID() error = %v", err)
	}
	deletable, ok := envelope.Source.(mailflow.DeletableSource)
	if !ok {
		t.Fatalf("source does not implement DeletableSource")
	}
	if err := deletable.Delete(context.Background()); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(deleter.deleted) != 1 || deleter.deleted[0].ID != "m3" {
		t.Fatalf("unexpected deleted refs: %+v", deleter.deleted)
	}
}

type readerEnvelopeReader struct {
	message provider.Message
	body    string
	err     error
}

func (r *readerEnvelopeReader) Me(context.Context) (provider.User, error) {
	return provider.User{}, nil
}
func (r *readerEnvelopeReader) Message(context.Context, string) (provider.Message, error) {
	if r.err != nil {
		return provider.Message{}, r.err
	}
	return r.message, nil
}
func (r *readerEnvelopeReader) FetchMIME(_ context.Context, _ string) (io.ReadCloser, error) {
	if r.err != nil {
		return nil, r.err
	}
	return io.NopCloser(strings.NewReader(r.body)), nil
}
func (r *readerEnvelopeReader) DeltaCreatedMessages(context.Context, string, string) ([]provider.Message, string, error) {
	return nil, "", errors.New("not implemented")
}
func (r *readerEnvelopeReader) FirstMessageInFolder(context.Context, string) (provider.Message, bool, error) {
	if r.err != nil {
		return provider.Message{}, false, r.err
	}
	if strings.TrimSpace(r.message.ID) == "" {
		return provider.Message{}, false, nil
	}
	return r.message, true, nil
}
func (r *readerEnvelopeReader) LatestMessagesInFolder(context.Context, string, int, int) ([]provider.Message, error) {
	return nil, errors.New("not implemented")
}
