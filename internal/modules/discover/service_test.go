package discover

import (
	"context"
	"io"
	"strings"
	"testing"

	"mimecrypt/internal/modules/process"
	"mimecrypt/internal/provider"
)

type fakeMailClient struct {
	messages []provider.Message
	delta    string
}

func (f fakeMailClient) DeltaCreatedMessages(context.Context, string, string) ([]provider.Message, string, error) {
	return f.messages, f.delta, nil
}

func (f fakeMailClient) FirstMessageInFolder(context.Context, string) (provider.Message, bool, error) {
	if len(f.messages) == 0 {
		return provider.Message{}, false, nil
	}
	return f.messages[0], true, nil
}

func (fakeMailClient) Me(context.Context) (provider.User, error) {
	return provider.User{}, nil
}

func (fakeMailClient) Message(context.Context, string) (provider.Message, error) {
	return provider.Message{}, nil
}

func (fakeMailClient) FetchMIME(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

type fakeProcessor struct {
	processed []string
}

func (f *fakeProcessor) Run(_ context.Context, req process.Request) (process.Result, error) {
	f.processed = append(f.processed, req.MessageID)
	return process.Result{MessageID: req.MessageID}, nil
}

func TestRunCycleSkipsBootstrapMessages(t *testing.T) {
	t.Parallel()

	statePath := t.TempDir() + "/sync.json"
	processor := &fakeProcessor{}
	service := Service{
		Client: fakeMailClient{
			messages: []provider.Message{{ID: "m1"}, {ID: "m2"}},
			delta:    "delta-1",
		},
		Processor: processor,
	}

	result, err := service.RunCycle(context.Background(), Request{
		Folder:    "inbox",
		StatePath: statePath,
		Process: process.Request{
			OutputDir: "output",
		},
	})
	if err != nil {
		t.Fatalf("RunCycle() error = %v", err)
	}
	if !result.Bootstrapped || result.Skipped != 2 || result.Processed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(processor.processed) != 0 {
		t.Fatalf("expected no processed messages, got %v", processor.processed)
	}
}

func TestDebugFirstRoutesSingleMessage(t *testing.T) {
	t.Parallel()

	processor := &fakeProcessor{}
	service := Service{
		Client: fakeMailClient{
			messages: []provider.Message{{ID: "m1"}},
		},
		Processor: processor,
	}

	result, err := service.DebugFirst(context.Background(), Request{
		Folder: "inbox",
		Process: process.Request{
			OutputDir: "output",
		},
	})
	if err != nil {
		t.Fatalf("DebugFirst() error = %v", err)
	}
	if !result.Found || result.Process.MessageID != "m1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(processor.processed) != 1 || processor.processed[0] != "m1" {
		t.Fatalf("unexpected processed messages: %v", processor.processed)
	}
}
