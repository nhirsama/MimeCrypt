package mailflow

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"mimecrypt/internal/provider"
)

type memoryStore struct {
	states map[string]TxState
}

func (s *memoryStore) Load(_ context.Context, key string) (TxState, bool, error) {
	if s.states == nil {
		s.states = make(map[string]TxState)
	}
	state, ok := s.states[key]
	return cloneState(state), ok, nil
}

func (s *memoryStore) Save(_ context.Context, state TxState) error {
	if s.states == nil {
		s.states = make(map[string]TxState)
	}
	s.states[state.Key] = cloneState(state)
	return nil
}

func cloneState(state TxState) TxState {
	cloned := state
	if state.Deliveries != nil {
		cloned.Deliveries = make(map[string]DeliveryReceipt, len(state.Deliveries))
		for key, receipt := range state.Deliveries {
			cloned.Deliveries[key] = receipt
		}
	}
	return cloned
}

type fakeProcessor struct {
	calls  int
	result ProcessedMail
	err    error
}

func (p *fakeProcessor) Process(context.Context, MailEnvelope) (ProcessedMail, error) {
	p.calls++
	if p.err != nil {
		return ProcessedMail{}, p.err
	}
	return p.result, nil
}

type fakeConsumer struct {
	calls   int
	receipt DeliveryReceipt
	err     error
}

func (c *fakeConsumer) Consume(context.Context, ConsumeRequest) (DeliveryReceipt, error) {
	c.calls++
	if c.err != nil {
		return DeliveryReceipt{}, c.err
	}
	return c.receipt, nil
}

type fakeSource struct {
	deleteCalls int
	ackCalls    int
	err         error
	ackErr      error
	semantics   provider.DeleteSemantics
}

func (s *fakeSource) Acknowledge(context.Context) error {
	s.ackCalls++
	return s.ackErr
}

func (s *fakeSource) Delete(context.Context) error {
	s.deleteCalls++
	return s.err
}

func (s *fakeSource) DeleteSemantics() provider.DeleteSemantics {
	if s == nil || s.semantics == "" {
		return provider.DeleteSemanticsHard
	}
	return s.semantics
}

func TestCoordinatorDeletesSourceWhenSameStoreReceiptCommitted(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	source := &fakeSource{}
	consumer := &fakeConsumer{
		receipt: DeliveryReceipt{
			ID:       "msg-1",
			Consumer: "archive",
			Store: StoreRef{
				Driver:  "imap",
				Account: "archive@example.com",
				Mailbox: "Encrypted",
			},
			Verified: true,
		},
	}
	processor := &fakeProcessor{
		result: ProcessedMail{
			Trace: MailTrace{
				TransactionKey: "tx-1",
				SourceName:     "office_inbox",
				SourceStore: StoreRef{
					Driver:  "imap",
					Account: "archive@example.com",
					Mailbox: "Encrypted",
				},
			},
			Plan: ExecutionPlan{
				Targets: []DeliveryTarget{{
					Name:     "archive-main",
					Consumer: "archive",
					Artifact: "primary",
					Required: true,
				}},
				DeleteSource: DeleteSourcePolicy{
					Enabled:          true,
					RequireSameStore: true,
				},
			},
			Artifacts: map[string]MailArtifact{
				"primary": {Name: "primary", MIME: bytesOpener("encrypted")},
			},
		},
	}

	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{"archive": consumer},
	}

	result, err := coordinator.Run(context.Background(), MailEnvelope{
		MIME: bytesOpener("source"),
		Trace: MailTrace{
			TransactionKey: "tx-1",
		},
		Source: source,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Completed {
		t.Fatalf("Completed = false, want true")
	}
	if result.Skipped {
		t.Fatalf("Skipped = true, want false")
	}
	if !result.SourceDeleted {
		t.Fatalf("SourceDeleted = false, want true")
	}
	if !result.SourceAcked {
		t.Fatalf("SourceAcked = false, want true")
	}
	if source.deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", source.deleteCalls)
	}
	if source.ackCalls != 1 {
		t.Fatalf("ackCalls = %d, want 1", source.ackCalls)
	}
	if consumer.calls != 1 {
		t.Fatalf("consumer calls = %d, want 1", consumer.calls)
	}
}

func TestCoordinatorKeepsSourceWhenStoreDiffers(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	source := &fakeSource{}
	consumer := &fakeConsumer{
		receipt: DeliveryReceipt{
			ID:       "msg-2",
			Consumer: "archive",
			Store: StoreRef{
				Driver:  "imap",
				Account: "other@example.com",
				Mailbox: "Encrypted",
			},
		},
	}
	processor := &fakeProcessor{
		result: ProcessedMail{
			Trace: MailTrace{
				TransactionKey: "tx-2",
				SourceStore: StoreRef{
					Driver:  "imap",
					Account: "archive@example.com",
					Mailbox: "Encrypted",
				},
			},
			Plan: ExecutionPlan{
				Targets: []DeliveryTarget{{
					Name:     "archive-main",
					Consumer: "archive",
					Artifact: "primary",
					Required: true,
				}},
				DeleteSource: DeleteSourcePolicy{
					Enabled:          true,
					RequireSameStore: true,
				},
			},
			Artifacts: map[string]MailArtifact{
				"primary": {Name: "primary", MIME: bytesOpener("encrypted")},
			},
		},
	}

	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{"archive": consumer},
	}

	result, err := coordinator.Run(context.Background(), MailEnvelope{
		MIME: bytesOpener("source"),
		Trace: MailTrace{
			TransactionKey: "tx-2",
		},
		Source: source,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !result.Completed {
		t.Fatalf("Completed = false, want true")
	}
	if result.Skipped {
		t.Fatalf("Skipped = true, want false")
	}
	if result.SourceDeleted {
		t.Fatalf("SourceDeleted = true, want false")
	}
	if !result.SourceAcked {
		t.Fatalf("SourceAcked = false, want true")
	}
	if source.deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0", source.deleteCalls)
	}
	if source.ackCalls != 1 {
		t.Fatalf("ackCalls = %d, want 1", source.ackCalls)
	}
}

func TestCoordinatorRejectsSoftDeleteSourceBeforeDelivery(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	source := &fakeSource{semantics: provider.DeleteSemanticsSoft}
	consumer := &fakeConsumer{
		receipt: DeliveryReceipt{
			ID:       "msg-soft",
			Consumer: "archive",
		},
	}
	processor := &fakeProcessor{
		result: ProcessedMail{
			Trace: MailTrace{
				TransactionKey: "tx-soft-delete",
			},
			Plan: ExecutionPlan{
				Targets: []DeliveryTarget{{
					Name:     "archive-main",
					Consumer: "archive",
					Artifact: "primary",
					Required: true,
				}},
				DeleteSource: DeleteSourcePolicy{
					Enabled:          true,
					RequireSameStore: true,
				},
			},
			Artifacts: map[string]MailArtifact{
				"primary": {Name: "primary", MIME: bytesOpener("encrypted")},
			},
		},
	}

	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{"archive": consumer},
	}

	_, err := coordinator.Run(context.Background(), MailEnvelope{
		MIME:   bytesOpener("source"),
		Trace:  MailTrace{TransactionKey: "tx-soft-delete"},
		Source: source,
	})
	if err == nil || !strings.Contains(err.Error(), "soft delete") {
		t.Fatalf("Run() error = %v, want soft delete rejection", err)
	}
	if consumer.calls != 0 {
		t.Fatalf("consumer calls = %d, want 0", consumer.calls)
	}
	if source.deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0", source.deleteCalls)
	}
	if source.ackCalls != 0 {
		t.Fatalf("ackCalls = %d, want 0", source.ackCalls)
	}
}

func TestCoordinatorAcknowledgesSourceWhenProcessorSkipsMessage(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	source := &fakeSource{}
	processor := &fakeProcessor{
		err: ErrSkipMessage,
	}

	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
	}

	result, err := coordinator.Run(context.Background(), MailEnvelope{
		MIME:   bytesOpener("source"),
		Trace:  MailTrace{TransactionKey: "tx-skip"},
		Source: source,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Completed {
		t.Fatalf("Completed = false, want true")
	}
	if !result.Skipped {
		t.Fatalf("Skipped = false, want true")
	}
	if !result.SourceAcked {
		t.Fatalf("SourceAcked = false, want true")
	}
	if source.ackCalls != 1 {
		t.Fatalf("ackCalls = %d, want 1", source.ackCalls)
	}
	if source.deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0", source.deleteCalls)
	}
}

func TestCoordinatorSkipsCommittedDeliveriesOnRetry(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	source := &fakeSource{}
	consumer := &fakeConsumer{
		receipt: DeliveryReceipt{
			ID:       "msg-3",
			Consumer: "archive",
			Store: StoreRef{
				Driver:  "imap",
				Account: "archive@example.com",
			},
		},
	}
	processor := &fakeProcessor{
		result: ProcessedMail{
			Trace: MailTrace{
				TransactionKey: "tx-3",
				SourceStore: StoreRef{
					Driver:  "imap",
					Account: "archive@example.com",
				},
			},
			Plan: ExecutionPlan{
				Targets: []DeliveryTarget{{
					Name:     "archive-main",
					Consumer: "archive",
					Artifact: "primary",
					Required: true,
				}},
				DeleteSource: DeleteSourcePolicy{
					Enabled:          true,
					RequireSameStore: true,
				},
			},
			Artifacts: map[string]MailArtifact{
				"primary": {Name: "primary", MIME: bytesOpener("encrypted")},
			},
		},
	}

	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{"archive": consumer},
	}

	envelope := MailEnvelope{
		MIME: bytesOpener("source"),
		Trace: MailTrace{
			TransactionKey: "tx-3",
		},
		Source: source,
	}
	if _, err := coordinator.Run(context.Background(), envelope); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if _, err := coordinator.Run(context.Background(), envelope); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	if consumer.calls != 1 {
		t.Fatalf("consumer calls = %d, want 1", consumer.calls)
	}
	if source.deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", source.deleteCalls)
	}
	if source.ackCalls != 1 {
		t.Fatalf("ackCalls = %d, want 1", source.ackCalls)
	}
}

func TestCoordinatorReturnsErrorWhenRequiredDeliveryFails(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	source := &fakeSource{}
	consumer := &fakeConsumer{err: errors.New("archive unavailable")}
	processor := &fakeProcessor{
		result: ProcessedMail{
			Trace: MailTrace{TransactionKey: "tx-4"},
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
	}

	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{"archive": consumer},
	}

	_, err := coordinator.Run(context.Background(), MailEnvelope{
		MIME: bytesOpener("source"),
		Trace: MailTrace{
			TransactionKey: "tx-4",
		},
		Source: source,
	})
	if err == nil || !strings.Contains(err.Error(), "archive unavailable") {
		t.Fatalf("Run() error = %v, want archive unavailable", err)
	}
	if source.deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0", source.deleteCalls)
	}
}

func TestCoordinatorFailsWhenSourceAcknowledgeFails(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	source := &fakeSource{ackErr: errors.New("ack failed")}
	processor := &fakeProcessor{
		result: ProcessedMail{
			Trace: MailTrace{TransactionKey: "tx-7"},
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
	}
	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{"archive": &fakeConsumer{}},
	}

	_, err := coordinator.Run(context.Background(), MailEnvelope{
		MIME: bytesOpener("source"),
		Trace: MailTrace{
			TransactionKey: "tx-7",
		},
		Source: source,
	})
	if err == nil || !strings.Contains(err.Error(), "ack failed") {
		t.Fatalf("Run() error = %v, want ack failed", err)
	}
}

func TestCoordinatorFinalizesCommittedStateWithoutReprocessing(t *testing.T) {
	t.Parallel()

	store := &memoryStore{
		states: map[string]TxState{
			"tx-8": {
				Key: "tx-8",
				Trace: MailTrace{
					TransactionKey: "tx-8",
				},
				Plan: ExecutionPlan{
					Targets: []DeliveryTarget{{
						Name:     "archive-main",
						Consumer: "archive",
						Artifact: "primary",
						Required: true,
					}},
				},
				Deliveries: map[string]DeliveryReceipt{
					"archive-main": {
						Target:   "archive-main",
						Consumer: "archive",
						ID:       "msg-8",
					},
				},
			},
		},
	}
	source := &fakeSource{}
	processor := &fakeProcessor{
		err: errors.New("processor should not run"),
	}
	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{"archive": &fakeConsumer{}},
	}

	result, err := coordinator.Run(context.Background(), MailEnvelope{
		MIME: bytesOpener("source"),
		Trace: MailTrace{
			TransactionKey: "tx-8",
		},
		Source: source,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Completed || !result.SourceAcked {
		t.Fatalf("unexpected result: %+v", result)
	}
	if processor.calls != 0 {
		t.Fatalf("processor calls = %d, want 0", processor.calls)
	}
	if source.ackCalls != 1 {
		t.Fatalf("ackCalls = %d, want 1", source.ackCalls)
	}
}

func TestCoordinatorRejectsPlanDriftOnRetry(t *testing.T) {
	t.Parallel()

	store := &memoryStore{
		states: map[string]TxState{
			"tx-5": {
				Key: "tx-5",
				Trace: MailTrace{
					TransactionKey: "tx-5",
				},
				Plan: ExecutionPlan{
					Targets: []DeliveryTarget{{
						Name:     "archive-main",
						Consumer: "archive",
						Artifact: "primary",
						Required: true,
					}},
				},
				Deliveries: make(map[string]DeliveryReceipt),
			},
		},
	}
	processor := &fakeProcessor{
		result: ProcessedMail{
			Trace: MailTrace{
				TransactionKey: "tx-5",
			},
			Plan: ExecutionPlan{
				Targets: []DeliveryTarget{{
					Name:     "archive-secondary",
					Consumer: "archive",
					Artifact: "primary",
					Required: true,
				}},
			},
			Artifacts: map[string]MailArtifact{
				"primary": {Name: "primary", MIME: bytesOpener("encrypted")},
			},
		},
	}

	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{"archive": &fakeConsumer{}},
	}

	_, err := coordinator.Run(context.Background(), MailEnvelope{
		MIME: bytesOpener("source"),
		Trace: MailTrace{
			TransactionKey: "tx-5",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "execution plan") {
		t.Fatalf("Run() error = %v, want execution plan drift", err)
	}
}

func TestProcessedMailValidateRequiresTargetArtifacts(t *testing.T) {
	t.Parallel()

	err := (ProcessedMail{
		Trace: MailTrace{
			TransactionKey: "tx-6",
		},
		Plan: ExecutionPlan{
			Targets: []DeliveryTarget{{
				Name:     "archive-main",
				Consumer: "archive",
				Artifact: "missing",
				Required: true,
			}},
		},
		Artifacts: map[string]MailArtifact{
			"primary": {Name: "primary", MIME: bytesOpener("encrypted")},
		},
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Validate() error = %v, want missing artifact", err)
	}
}

func bytesOpener(content string) MIMEOpener {
	return func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(content))), nil
	}
}
