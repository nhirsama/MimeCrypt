package cli

import (
	"context"
	"fmt"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/mailflow/adapters"
)

func buildMailflowEnvelopeBuilder(ctx context.Context, cfg appconfig.Config, resolved resolvedMailflowTopology) (*adapters.ReaderEnvelopeBuilder, error) {
	sourceClients, err := buildSourceProviderClients(cfg, resolved.Source)
	if err != nil {
		return nil, err
	}
	sourceStore, err := buildMailflowSourceStore(ctx, cfg, sourceClients.Reader, resolved.Source, resolved.Route.DeleteSource.Enabled)
	if err != nil {
		return nil, err
	}
	return &adapters.ReaderEnvelopeBuilder{
		Name:    resolved.Source.Name,
		Driver:  normalizeDriver(resolved.Source.Driver, "imap"),
		Folder:  resolved.Source.Folder,
		Store:   sourceStore,
		Reader:  sourceClients.Reader,
		Deleter: sourceClients.Deleter,
	}, nil
}

func runMailflowMessageByID(ctx context.Context, cfg appconfig.Config, resolved resolvedMailflowTopology, messageID string) (mailflowSummary, error) {
	builder, err := buildMailflowEnvelopeBuilder(ctx, cfg, resolved)
	if err != nil {
		return mailflowSummary{}, err
	}
	envelope, err := builder.EnvelopeForID(ctx, messageID, resolved.Source.Folder)
	if err != nil {
		return mailflowSummary{}, err
	}
	return runMailflowEnvelope(ctx, cfg, resolved, envelope)
}

func runMailflowFirstMessage(ctx context.Context, cfg appconfig.Config, resolved resolvedMailflowTopology) (mailflowSummary, bool, error) {
	sourceClients, err := buildSourceProviderClients(cfg, resolved.Source)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	message, found, err := sourceClients.Reader.FirstMessageInFolder(ctx, resolved.Source.Folder)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	if !found {
		return mailflowSummary{}, false, nil
	}
	builder, err := buildMailflowEnvelopeBuilder(ctx, cfg, resolved)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	envelope, err := builder.EnvelopeForMessage(ctx, message)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	summary, err := runMailflowEnvelope(ctx, cfg, resolved, envelope)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	return summary, true, nil
}

func runMailflowEnvelope(ctx context.Context, cfg appconfig.Config, resolved resolvedMailflowTopology, envelope mailflow.MailEnvelope) (mailflowSummary, error) {
	coordinator, err := buildMailflowCoordinatorWithStore(ctx, cfg, resolved, &mailflow.MemoryStateStore{})
	if err != nil {
		return mailflowSummary{}, err
	}
	result, err := coordinator.Run(ctx, envelope)
	if err != nil {
		return mailflowSummary{}, err
	}
	summary, err := summarizeMailflowResult(result)
	if err != nil {
		return mailflowSummary{}, err
	}
	if summary.MessageID == "" {
		return mailflowSummary{}, fmt.Errorf("mailflow 结果缺少 message id")
	}
	return summary, nil
}
