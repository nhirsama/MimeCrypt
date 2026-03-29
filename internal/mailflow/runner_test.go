package mailflow

import (
	"context"
	"errors"
	"testing"
)

type fakeProducer struct {
	envelope MailEnvelope
	err      error
}

func (p *fakeProducer) Next(context.Context) (MailEnvelope, error) {
	if p.err != nil {
		return MailEnvelope{}, p.err
	}
	return p.envelope, nil
}

func TestRunnerRunOnceReturnsProcessedFalseWhenNoMessages(t *testing.T) {
	t.Parallel()

	runner := &Runner{
		Producer:    &fakeProducer{err: ErrNoMessages},
		Coordinator: &Coordinator{},
	}

	_, processed, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if processed {
		t.Fatalf("processed = true, want false")
	}
}

func TestRunnerRunOnceRunsCoordinator(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	source := &fakeSource{}
	runner := &Runner{
		Producer: &fakeProducer{
			envelope: MailEnvelope{
				MIME: bytesOpener("source"),
				Trace: MailTrace{
					TransactionKey: "tx-runner",
				},
				Source: source,
			},
		},
		Coordinator: &Coordinator{
			Processor: &fakeProcessor{
				result: ProcessedMail{
					Trace: MailTrace{TransactionKey: "tx-runner"},
					Plan: ExecutionPlan{
						Targets: []DeliveryTarget{{
							Name:     "archive-main",
							Consumer: "archive",
							Artifact: "primary",
							Required: true,
						}},
					},
					Artifacts: map[string]MailArtifact{
						"primary": {Name: "primary", MIME: bytesOpener("encrypted")},
					},
				},
			},
			Store:     store,
			Consumers: map[string]Consumer{"archive": &fakeConsumer{}},
		},
	}

	result, processed, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !processed {
		t.Fatalf("processed = false, want true")
	}
	if !result.Completed {
		t.Fatalf("Completed = false, want true")
	}
}

func TestRunnerRunOnceReturnsProducerError(t *testing.T) {
	t.Parallel()

	runner := &Runner{
		Producer:    &fakeProducer{err: errors.New("boom")},
		Coordinator: &Coordinator{},
	}

	_, processed, err := runner.RunOnce(context.Background())
	if err == nil || err.Error() != "boom" {
		t.Fatalf("RunOnce() error = %v, want boom", err)
	}
	if processed {
		t.Fatalf("processed = true, want false")
	}
}
