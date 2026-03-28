package discover

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"mimecrypt/internal/modules/encrypt"
	"mimecrypt/internal/modules/process"
	"mimecrypt/internal/provider"
)

type fakeMailClient struct {
	messages   []provider.Message
	delta      string
	deltaCalls *int
}

func (f fakeMailClient) DeltaCreatedMessages(context.Context, string, string) ([]provider.Message, string, error) {
	if f.deltaCalls != nil {
		*f.deltaCalls++
	}
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

func (f fakeMailClient) LatestMessagesInFolder(context.Context, string, int, int) ([]provider.Message, error) {
	return f.messages, nil
}

type fakeProcessor struct {
	processed []string
	errByID   map[string]error
}

func (f *fakeProcessor) Run(_ context.Context, req process.Request) (process.Result, error) {
	f.processed = append(f.processed, req.Source.ID)
	if err, ok := f.errByID[req.Source.ID]; ok {
		return process.Result{}, err
	}
	return process.Result{MessageID: req.Source.ID}, nil
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

func TestRunCycleSkipsAlreadyEncryptedErrorAndContinues(t *testing.T) {
	t.Parallel()

	statePath := t.TempDir() + "/sync.json"
	processor := &fakeProcessor{
		errByID: map[string]error{
			"m2": encrypt.AlreadyEncryptedError{Format: "pgp-mime"},
		},
	}
	service := Service{
		Client: fakeMailClient{
			messages: []provider.Message{{ID: "m1"}, {ID: "m2"}, {ID: "m3"}},
			delta:    "delta-2",
		},
		Processor: processor,
	}

	result, err := service.RunCycle(context.Background(), Request{
		Folder:          "inbox",
		StatePath:       statePath,
		IncludeExisting: true,
		Process: process.Request{
			OutputDir: "output",
		},
	})
	if err != nil {
		t.Fatalf("RunCycle() error = %v", err)
	}
	if result.Processed != 2 {
		t.Fatalf("expected Processed=2, got %+v", result)
	}
	if got := strings.Join(processor.processed, ","); got != "m1,m2,m3" {
		t.Fatalf("unexpected processed sequence: %s", got)
	}
}

func TestRunCycleStopsOnNonEncryptedError(t *testing.T) {
	t.Parallel()

	statePath := t.TempDir() + "/sync.json"
	processor := &fakeProcessor{
		errByID: map[string]error{
			"m2": errors.New("boom"),
		},
	}
	service := Service{
		Client: fakeMailClient{
			messages: []provider.Message{{ID: "m1"}, {ID: "m2"}, {ID: "m3"}},
			delta:    "delta-3",
		},
		Processor: processor,
	}

	_, err := service.RunCycle(context.Background(), Request{
		Folder:          "inbox",
		StatePath:       statePath,
		IncludeExisting: true,
		Process: process.Request{
			OutputDir: "output",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "处理邮件 m2 失败") {
		t.Fatalf("expected wrapped error for m2, got %v", err)
	}
	if got := strings.Join(processor.processed, ","); got != "m1,m2" {
		t.Fatalf("unexpected processed sequence: %s", got)
	}
}

func TestRunCycleResumesPendingMessagesWithoutReprocessingSuccesses(t *testing.T) {
	t.Parallel()

	statePath := t.TempDir() + "/sync.json"
	deltaCalls := 0
	processor := &fakeProcessor{
		errByID: map[string]error{
			"m2": errors.New("boom"),
		},
	}
	service := Service{
		Client: fakeMailClient{
			messages:   []provider.Message{{ID: "m1"}, {ID: "m2"}, {ID: "m3"}},
			delta:      "delta-4",
			deltaCalls: &deltaCalls,
		},
		Processor: processor,
	}

	_, err := service.RunCycle(context.Background(), Request{
		Folder:          "inbox",
		StatePath:       statePath,
		IncludeExisting: true,
		Process: process.Request{
			OutputDir: "output",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "处理邮件 m2 失败") {
		t.Fatalf("expected wrapped error for m2, got %v", err)
	}
	if got := strings.Join(processor.processed, ","); got != "m1,m2" {
		t.Fatalf("unexpected processed sequence after first run: %s", got)
	}
	if deltaCalls != 1 {
		t.Fatalf("delta calls after first run = %d, want 1", deltaCalls)
	}

	delete(processor.errByID, "m2")

	result, err := service.RunCycle(context.Background(), Request{
		Folder:          "inbox",
		StatePath:       statePath,
		IncludeExisting: true,
		Process: process.Request{
			OutputDir: "output",
		},
	})
	if err != nil {
		t.Fatalf("second RunCycle() error = %v", err)
	}
	if result.Processed != 2 {
		t.Fatalf("expected Processed=2 on resume, got %+v", result)
	}
	if got := strings.Join(processor.processed, ","); got != "m1,m2,m2,m3" {
		t.Fatalf("unexpected processed sequence after resume: %s", got)
	}
	if deltaCalls != 1 {
		t.Fatalf("delta calls after resume = %d, want 1", deltaCalls)
	}
}
