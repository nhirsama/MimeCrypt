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
	request ConsumeRequest
	receipt DeliveryReceipt
	err     error
}

func (c *fakeConsumer) Consume(_ context.Context, req ConsumeRequest) (DeliveryReceipt, error) {
	c.calls++
	c.request = req
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

type fakeFinalizingSource struct {
	*fakeSource
	finalizeCalls int
	finalizeErr   error
}

func (s *fakeFinalizingSource) FinalizeAcknowledge(context.Context) error {
	s.finalizeCalls++
	return s.finalizeErr
}

type failSaveStore struct {
	memoryStore
	failOnSave int
	saveCalls  int
	err        error
}

func (s *failSaveStore) Save(ctx context.Context, state TxState) error {
	s.saveCalls++
	if s.failOnSave > 0 && s.saveCalls == s.failOnSave {
		return s.err
	}
	return s.memoryStore.Save(ctx, state)
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
			Mail: MailObject{Name: "primary", MIME: bytesOpener("encrypted")},
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
		Source:                source,
		SourceDeleteSemantics: provider.DeleteSemanticsHard,
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

func TestCoordinatorDeliversNoOpStyleProcessorOutput(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	source := &fakeSource{}
	consumer := &fakeConsumer{
		receipt: DeliveryReceipt{
			ID:       "msg-noop",
			Consumer: "archive",
			Verified: true,
		},
	}
	processor := &fakeProcessor{
		result: ProcessedMail{
			Trace: MailTrace{
				TransactionKey: "tx-noop",
				Attributes: map[string]string{
					"format": "noop",
				},
			},
			Plan: ExecutionPlan{
				Targets: []DeliveryTarget{{
					Name:     "archive-main",
					Consumer: "archive",
					Artifact: "primary",
					Required: true,
				}},
			},
			Mail: MailObject{Name: "primary", MIME: bytesOpener("original-mime")},
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
			TransactionKey: "tx-noop",
		},
		Source: source,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Completed || result.Skipped {
		t.Fatalf("result = %+v, want completed non-skipped flow", result)
	}
	if consumer.calls != 1 {
		t.Fatalf("consumer calls = %d, want 1", consumer.calls)
	}
	if consumer.request.Trace.Attributes["format"] != "noop" {
		t.Fatalf("trace format = %q, want noop", consumer.request.Trace.Attributes["format"])
	}
	reader, err := consumer.request.Mail.MIME()
	if err != nil {
		t.Fatalf("Mail.MIME() error = %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	_ = reader.Close()
	if string(data) != "original-mime" {
		t.Fatalf("mail data = %q, want original-mime", string(data))
	}
}

func TestCoordinatorDeliversBackupTargetFromSameUnifiedMailObject(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	primary := &fakeConsumer{
		receipt: DeliveryReceipt{
			ID:       "msg-primary",
			Consumer: "archive",
			Verified: true,
		},
	}
	backup := &fakeConsumer{
		receipt: DeliveryReceipt{
			ID:       "/backup/msg-primary.pgp",
			Consumer: "archive-backup",
			Store: StoreRef{
				Driver:  "backup",
				Account: "/backup",
			},
			Verified: true,
		},
	}
	processor := &fakeProcessor{
		result: ProcessedMail{
			Trace: MailTrace{
				TransactionKey: "tx-backup-artifact",
				Attributes: map[string]string{
					"format": "pgp-mime",
				},
			},
			Plan: ExecutionPlan{
				Targets: []DeliveryTarget{
					{
						Name:     "archive-main",
						Consumer: "archive",
						Artifact: "primary",
						Required: true,
					},
					{
						Name:     "archive-backup",
						Consumer: "archive-backup",
						Artifact: "backup",
						Required: false,
					},
				},
			},
			Mail: MailObject{
				Name: "mail",
				MIME: bytesOpener("processed-mime"),
				Attributes: map[string]string{
					"format": "pgp-mime",
				},
			},
		},
	}

	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{
			"archive":        primary,
			"archive-backup": backup,
		},
	}

	result, err := coordinator.Run(context.Background(), MailEnvelope{
		MIME: bytesOpener("source"),
		Trace: MailTrace{
			TransactionKey: "tx-backup-artifact",
		},
		Source: &fakeSource{},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Completed {
		t.Fatalf("Completed = false, want true")
	}
	if got := result.Trace.Attributes["backup_path"]; got != "/backup/msg-primary.pgp" {
		t.Fatalf("backup_path = %q, want /backup/msg-primary.pgp", got)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("consumer calls = primary:%d backup:%d, want 1 each", primary.calls, backup.calls)
	}
	if backup.request.Target.Artifact != "backup" {
		t.Fatalf("backup target artifact = %q, want backup", backup.request.Target.Artifact)
	}

	for name, request := range map[string]ConsumeRequest{
		"primary": primary.request,
		"backup":  backup.request,
	} {
		reader, err := request.Mail.MIME()
		if err != nil {
			t.Fatalf("%s Mail.MIME() error = %v", name, err)
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatalf("%s ReadAll() error = %v", name, err)
		}
		if string(data) != "processed-mime" {
			t.Fatalf("%s mail data = %q, want processed-mime", name, string(data))
		}
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
			Mail: MailObject{Name: "primary", MIME: bytesOpener("encrypted")},
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
		Source:                source,
		SourceDeleteSemantics: provider.DeleteSemanticsHard,
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
			Mail: MailObject{Name: "primary", MIME: bytesOpener("encrypted")},
		},
	}

	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{"archive": consumer},
	}

	_, err := coordinator.Run(context.Background(), MailEnvelope{
		MIME:                  bytesOpener("source"),
		Trace:                 MailTrace{TransactionKey: "tx-soft-delete"},
		Source:                source,
		SourceDeleteSemantics: provider.DeleteSemanticsSoft,
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
		MIME:                  bytesOpener("source"),
		Trace:                 MailTrace{TransactionKey: "tx-skip"},
		Source:                source,
		SourceDeleteSemantics: provider.DeleteSemanticsHard,
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
			Mail: MailObject{Name: "primary", MIME: bytesOpener("encrypted")},
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
		Source:                source,
		SourceDeleteSemantics: provider.DeleteSemanticsHard,
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
			Mail: MailObject{Name: "primary", MIME: bytesOpener("encrypted")},
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
		Source:                source,
		SourceDeleteSemantics: provider.DeleteSemanticsHard,
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
			Mail: MailObject{Name: "primary", MIME: bytesOpener("encrypted")},
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
		Source:                source,
		SourceDeleteSemantics: provider.DeleteSemanticsHard,
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
		Source:                source,
		SourceDeleteSemantics: provider.DeleteSemanticsHard,
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

func TestCoordinatorRecoversWhenAckStateSaveFailsAfterPrepare(t *testing.T) {
	t.Parallel()

	store := &failSaveStore{
		failOnSave: 3,
		err:        errors.New("state store unavailable"),
	}
	source := &fakeFinalizingSource{fakeSource: &fakeSource{}}
	consumer := &fakeConsumer{
		receipt: DeliveryReceipt{
			ID:       "msg-recover",
			Consumer: "archive",
		},
	}
	processor := &fakeProcessor{
		result: ProcessedMail{
			Trace: MailTrace{TransactionKey: "tx-recover-ack"},
			Plan: ExecutionPlan{
				Targets: []DeliveryTarget{{
					Name:     "archive-main",
					Consumer: "archive",
					Artifact: "primary",
					Required: true,
				}},
			},
			Mail: MailObject{Name: "primary", MIME: bytesOpener("encrypted")},
		},
	}
	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{"archive": consumer},
	}
	envelope := MailEnvelope{
		MIME:                  bytesOpener("source"),
		Trace:                 MailTrace{TransactionKey: "tx-recover-ack"},
		Source:                source,
		SourceDeleteSemantics: provider.DeleteSemanticsHard,
	}

	if _, err := coordinator.Run(context.Background(), envelope); err == nil || !strings.Contains(err.Error(), "state store unavailable") {
		t.Fatalf("first Run() error = %v, want save failure", err)
	}
	if source.ackCalls != 1 {
		t.Fatalf("ackCalls after first run = %d, want 1", source.ackCalls)
	}
	if source.finalizeCalls != 0 {
		t.Fatalf("finalizeCalls after first run = %d, want 0", source.finalizeCalls)
	}
	if processor.calls != 1 {
		t.Fatalf("processor calls after first run = %d, want 1", processor.calls)
	}

	store.failOnSave = 0
	result, err := coordinator.Run(context.Background(), envelope)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if !result.Completed || !result.SourceAcked {
		t.Fatalf("unexpected result after recovery: %+v", result)
	}
	if consumer.calls != 1 {
		t.Fatalf("consumer calls = %d, want 1", consumer.calls)
	}
	if processor.calls != 1 {
		t.Fatalf("processor calls = %d, want 1 on recovery", processor.calls)
	}
	if source.ackCalls != 2 {
		t.Fatalf("ackCalls after recovery = %d, want 2", source.ackCalls)
	}
	if source.finalizeCalls != 1 {
		t.Fatalf("finalizeCalls after recovery = %d, want 1", source.finalizeCalls)
	}
}

func TestCoordinatorFinalizesRecoveredCompletedSourceWithoutReprocessing(t *testing.T) {
	t.Parallel()

	store := &memoryStore{
		states: map[string]TxState{
			"tx-complete-recover": {
				Key: "tx-complete-recover",
				Trace: MailTrace{
					TransactionKey: "tx-complete-recover",
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
						ID:       "msg-complete",
					},
				},
				SourceAcked: true,
				Completed:   true,
			},
		},
	}
	source := &fakeFinalizingSource{fakeSource: &fakeSource{}}
	processor := &fakeProcessor{err: errors.New("processor should not run")}
	coordinator := &Coordinator{
		Processor: processor,
		Store:     store,
		Consumers: map[string]Consumer{"archive": &fakeConsumer{}},
	}

	result, err := coordinator.Run(context.Background(), MailEnvelope{
		MIME:                  bytesOpener("source"),
		Trace:                 MailTrace{TransactionKey: "tx-complete-recover"},
		Source:                source,
		SourceDeleteSemantics: provider.DeleteSemanticsHard,
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
	if source.ackCalls != 0 {
		t.Fatalf("ackCalls = %d, want 0", source.ackCalls)
	}
	if source.finalizeCalls != 1 {
		t.Fatalf("finalizeCalls = %d, want 1", source.finalizeCalls)
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
			Mail: MailObject{Name: "primary", MIME: bytesOpener("encrypted")},
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

func TestProcessedMailValidateRequiresMailObject(t *testing.T) {
	t.Parallel()

	err := (ProcessedMail{
		Trace: MailTrace{
			TransactionKey: "tx-6",
		},
		Plan: ExecutionPlan{
			Targets: []DeliveryTarget{{
				Name:     "archive-main",
				Consumer: "archive",
				Artifact: "primary",
				Required: true,
			}},
		},
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "邮件对象") {
		t.Fatalf("Validate() error = %v, want missing mail object", err)
	}
}

func bytesOpener(content string) MIMEOpener {
	return func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(content))), nil
	}
}
