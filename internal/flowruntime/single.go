package flowruntime

import (
	"context"
	"fmt"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/provider"
)

type TransactionMode string

const (
	TransactionModePersistent TransactionMode = "persistent"
	TransactionModeEphemeral  TransactionMode = "ephemeral"
)

type SingleMessageRunner struct {
	run         SourceRun
	reader      provider.Reader
	builder     envelopeBuilder
	coordinator *mailflow.Coordinator
}

type envelopeBuilder interface {
	EnvelopeForID(ctx context.Context, messageID, fallbackFolder string) (mailflow.MailEnvelope, error)
	EnvelopeForMessage(ctx context.Context, message provider.Message) (mailflow.MailEnvelope, error)
}

func BuildSingleMessageRunner(ctx context.Context, run SourceRun, mode TransactionMode) (*SingleMessageRunner, error) {
	source, err := buildSourceBundle(SourcePlan{
		Source: run.Source,
		Config: run.Config,
	})
	if err != nil {
		return nil, err
	}
	builder, err := buildEnvelopeBuilderFromSourceBundle(ctx, run, source)
	if err != nil {
		return nil, err
	}
	coordinator, err := buildCoordinatorForMode(ctx, run, mode)
	if err != nil {
		return nil, err
	}
	return &SingleMessageRunner{
		run:         run,
		reader:      source.Clients.Reader,
		builder:     builder,
		coordinator: coordinator,
	}, nil
}

func buildCoordinatorForMode(ctx context.Context, run SourceRun, mode TransactionMode) (*mailflow.Coordinator, error) {
	store, err := stateStoreForMode(run, mode)
	if err != nil {
		return nil, err
	}
	return buildCoordinatorWithStore(ctx, run, store)
}

func (r *SingleMessageRunner) RunMessageByID(ctx context.Context, messageID string) (mailflow.Result, error) {
	if r == nil || r.builder == nil {
		return mailflow.Result{}, fmt.Errorf("single message runner 未初始化")
	}
	envelope, err := r.builder.EnvelopeForID(ctx, messageID, r.run.Source.Folder)
	if err != nil {
		return mailflow.Result{}, err
	}
	return r.RunEnvelope(ctx, envelope)
}

func (r *SingleMessageRunner) RunFirstMessage(ctx context.Context) (mailflow.Result, bool, error) {
	if r == nil || r.reader == nil || r.builder == nil {
		return mailflow.Result{}, false, fmt.Errorf("single message runner 未初始化")
	}
	message, found, err := r.reader.FirstMessageInFolder(ctx, r.run.Source.Folder)
	if err != nil {
		return mailflow.Result{}, false, err
	}
	if !found {
		return mailflow.Result{}, false, nil
	}
	envelope, err := r.builder.EnvelopeForMessage(ctx, message)
	if err != nil {
		return mailflow.Result{}, false, err
	}
	result, err := r.RunEnvelope(ctx, envelope)
	if err != nil {
		return mailflow.Result{}, false, err
	}
	return result, true, nil
}

func (r *SingleMessageRunner) RunEnvelope(ctx context.Context, envelope mailflow.MailEnvelope) (mailflow.Result, error) {
	if r == nil || r.coordinator == nil {
		return mailflow.Result{}, fmt.Errorf("single message runner 未初始化")
	}
	return r.coordinator.Run(ctx, envelope)
}

func stateStoreForMode(run SourceRun, mode TransactionMode) (mailflow.StateStore, error) {
	switch mode {
	case "", TransactionModePersistent:
		return mailflow.FileStateStore{Dir: run.Route.StateDir}, nil
	case TransactionModeEphemeral:
		return &mailflow.MemoryStateStore{}, nil
	default:
		return nil, fmt.Errorf("不支持的事务模式: %s", mode)
	}
}
