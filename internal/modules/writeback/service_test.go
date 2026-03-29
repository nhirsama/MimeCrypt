package writeback

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"mimecrypt/internal/provider"
)

type fakeProviderWriter struct {
	writeReq         provider.WriteRequest
	writeResult      provider.WriteResult
	writeErr         error
	reconcileReq     provider.WriteRequest
	reconcileResult  provider.WriteResult
	reconcileFound   bool
	reconcileErr     error
	supportReconcile bool
}

func (f *fakeProviderWriter) WriteMessage(_ context.Context, req provider.WriteRequest) (provider.WriteResult, error) {
	f.writeReq = req
	if f.writeErr != nil {
		return provider.WriteResult{}, f.writeErr
	}
	return f.writeResult, nil
}

func (f *fakeProviderWriter) ReconcileMessage(_ context.Context, req provider.WriteRequest) (provider.WriteResult, bool, error) {
	f.reconcileReq = req
	if f.reconcileErr != nil {
		return provider.WriteResult{}, false, f.reconcileErr
	}
	return f.reconcileResult, f.reconcileFound, nil
}

type writeOnlyProviderWriter struct {
	writeReq    provider.WriteRequest
	writeResult provider.WriteResult
	writeErr    error
}

func (f *writeOnlyProviderWriter) WriteMessage(_ context.Context, req provider.WriteRequest) (provider.WriteResult, error) {
	f.writeReq = req
	if f.writeErr != nil {
		return provider.WriteResult{}, f.writeErr
	}
	return f.writeResult, nil
}

func TestRunRejectsMissingMessageID(t *testing.T) {
	t.Parallel()

	service := Service{}
	_, err := service.Run(context.Background(), Request{MIME: []byte("encrypted"), DeleteSource: true})
	if err == nil || !strings.Contains(err.Error(), "message id 不能为空") {
		t.Fatalf("Run() error = %v, want message id validation", err)
	}
}

func TestRunAllowsMissingMessageIDWhenDeleteDisabled(t *testing.T) {
	t.Parallel()

	writer := &fakeProviderWriter{}
	service := Service{Writer: writer}

	if _, err := service.Run(context.Background(), Request{MIME: []byte("encrypted")}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if writer.writeReq.DeleteSource {
		t.Fatalf("DeleteSource = true, want false")
	}
}

func TestRunRejectsMissingMIME(t *testing.T) {
	t.Parallel()

	service := Service{Writer: &fakeProviderWriter{}}
	_, err := service.Run(context.Background(), Request{
		Source: provider.MessageRef{ID: "m1"},
	})
	if err == nil || !strings.Contains(err.Error(), "回写 MIME 不能为空") {
		t.Fatalf("Run() error = %v, want missing MIME validation", err)
	}
}

func TestRunReturnsNotImplementedWhenWriterMissing(t *testing.T) {
	t.Parallel()

	service := Service{}
	_, err := service.Run(context.Background(), Request{
		Source: provider.MessageRef{ID: "m1"},
		MIME:   []byte("encrypted"),
	})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Run() error = %v, want ErrNotImplemented", err)
	}
}

func TestRunMapsProviderNotSupportedToNotImplemented(t *testing.T) {
	t.Parallel()

	service := Service{
		Writer: &fakeProviderWriter{writeErr: provider.ErrNotSupported},
	}
	_, err := service.Run(context.Background(), Request{
		Source: provider.MessageRef{ID: "m1"},
		MIME:   []byte("encrypted"),
	})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Run() error = %v, want ErrNotImplemented", err)
	}
}

func TestRunReturnsProviderError(t *testing.T) {
	t.Parallel()

	service := Service{
		Writer: &fakeProviderWriter{writeErr: errors.New("write failed")},
	}
	_, err := service.Run(context.Background(), Request{
		Source: provider.MessageRef{ID: "m1"},
		MIME:   []byte("encrypted"),
	})
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("Run() error = %v, want provider error", err)
	}
}

func TestRunForwardsRequestAndResult(t *testing.T) {
	t.Parallel()

	writer := &fakeProviderWriter{
		writeResult: provider.WriteResult{Verified: true},
	}
	service := Service{Writer: writer}

	result, err := service.Run(context.Background(), Request{
		Source:              provider.MessageRef{ID: "m1", FolderID: "source-folder"},
		MIME:                []byte("encrypted"),
		DestinationFolderID: "target-folder",
		Verify:              true,
		DeleteSource:        true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Verified {
		t.Fatalf("Verified = false, want true")
	}
	if writer.writeReq.Source.ID != "m1" {
		t.Fatalf("Source.ID = %q, want m1", writer.writeReq.Source.ID)
	}
	if writer.writeReq.DestinationFolderID != "target-folder" {
		t.Fatalf("DestinationFolderID = %q, want target-folder", writer.writeReq.DestinationFolderID)
	}
	if !writer.writeReq.Verify {
		t.Fatalf("Verify = false, want true")
	}
	if !writer.writeReq.DeleteSource {
		t.Fatalf("DeleteSource = false, want true")
	}
	if string(writer.writeReq.MIME) != "encrypted" {
		t.Fatalf("MIME = %q, want encrypted", string(writer.writeReq.MIME))
	}
}

func TestRunAcceptsMIMEOpener(t *testing.T) {
	t.Parallel()

	writer := &fakeProviderWriter{}
	service := Service{Writer: writer}

	result, err := service.Run(context.Background(), Request{
		Source: provider.MessageRef{ID: "m1"},
		MIMEOpener: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("encrypted-stream")), nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Verified {
		t.Fatalf("Verified = true, want false")
	}
	if writer.writeReq.MIMEOpener == nil {
		t.Fatalf("expected MIMEOpener to be forwarded")
	}
	reader, err := writer.writeReq.MIMEOpener()
	if err != nil {
		t.Fatalf("MIMEOpener() error = %v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "encrypted-stream" {
		t.Fatalf("MIMEOpener data = %q, want encrypted-stream", string(data))
	}
}

func TestReconcileReturnsNotImplementedWhenWriterMissing(t *testing.T) {
	t.Parallel()

	service := Service{}
	_, _, err := service.Reconcile(context.Background(), Request{
		Source: provider.MessageRef{ID: "m1"},
	})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Reconcile() error = %v, want ErrNotImplemented", err)
	}
}

func TestReconcileRejectsMissingMessageID(t *testing.T) {
	t.Parallel()

	service := Service{Writer: &fakeProviderWriter{}}
	_, _, err := service.Reconcile(context.Background(), Request{DeleteSource: true})
	if err == nil || !strings.Contains(err.Error(), "message id 不能为空") {
		t.Fatalf("Reconcile() error = %v, want message id validation", err)
	}
}

func TestReconcileReturnsNotImplementedWhenWriterDoesNotSupportReconcile(t *testing.T) {
	t.Parallel()

	service := Service{Writer: &writeOnlyProviderWriter{}}
	_, _, err := service.Reconcile(context.Background(), Request{
		Source: provider.MessageRef{ID: "m1"},
	})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Reconcile() error = %v, want ErrNotImplemented", err)
	}
}

func TestReconcileMapsProviderNotSupportedToNotImplemented(t *testing.T) {
	t.Parallel()

	service := Service{
		Writer: &fakeProviderWriter{reconcileErr: provider.ErrNotSupported},
	}
	_, _, err := service.Reconcile(context.Background(), Request{
		Source: provider.MessageRef{ID: "m1"},
	})
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Reconcile() error = %v, want ErrNotImplemented", err)
	}
}

func TestReconcileReturnsProviderError(t *testing.T) {
	t.Parallel()

	service := Service{
		Writer: &fakeProviderWriter{reconcileErr: errors.New("reconcile failed")},
	}
	_, _, err := service.Reconcile(context.Background(), Request{
		Source: provider.MessageRef{ID: "m1"},
	})
	if err == nil || !strings.Contains(err.Error(), "reconcile failed") {
		t.Fatalf("Reconcile() error = %v, want provider error", err)
	}
}

func TestReconcileForwardsRequestAndResult(t *testing.T) {
	t.Parallel()

	writer := &fakeProviderWriter{
		reconcileResult: provider.WriteResult{Verified: true},
		reconcileFound:  true,
	}
	service := Service{Writer: writer}

	result, found, err := service.Reconcile(context.Background(), Request{
		Source:              provider.MessageRef{ID: "m1", FolderID: "source-folder"},
		MIME:                []byte("encrypted"),
		DestinationFolderID: "target-folder",
		Verify:              true,
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if !result.Verified {
		t.Fatalf("Verified = false, want true")
	}
	if writer.reconcileReq.Source.ID != "m1" {
		t.Fatalf("Source.ID = %q, want m1", writer.reconcileReq.Source.ID)
	}
	if writer.reconcileReq.DestinationFolderID != "target-folder" {
		t.Fatalf("DestinationFolderID = %q, want target-folder", writer.reconcileReq.DestinationFolderID)
	}
	if !writer.reconcileReq.Verify {
		t.Fatalf("Verify = false, want true")
	}
}
