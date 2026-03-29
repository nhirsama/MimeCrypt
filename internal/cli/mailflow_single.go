package cli

import (
	"context"
	"fmt"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/mailflow/adapters"
	"mimecrypt/internal/provider"
)

func buildMailflowEnvelopeBuilder(ctx context.Context, cfg appconfig.Config, clients provider.Clients, deleteSource bool) (*adapters.ReaderEnvelopeBuilder, error) {
	sourceStore, err := buildMailflowSourceStore(ctx, cfg, clients.Reader, deleteSource)
	if err != nil {
		return nil, err
	}
	return &adapters.ReaderEnvelopeBuilder{
		Name:    "default",
		Driver:  normalizeDriver(cfg.Provider, "imap"),
		Folder:  cfg.Mail.Sync.Folder,
		Store:   sourceStore,
		Reader:  clients.Reader,
		Deleter: clients.Deleter,
	}, nil
}

func runMailflowMessageByID(ctx context.Context, cfg appconfig.Config, messageID string, writeBack, verifyWriteBack, deleteSource bool) (mailflowSummary, error) {
	clients, err := buildProviderClients(cfg)
	if err != nil {
		return mailflowSummary{}, err
	}
	builder, err := buildMailflowEnvelopeBuilder(ctx, cfg, clients, deleteSource)
	if err != nil {
		return mailflowSummary{}, err
	}
	envelope, err := builder.EnvelopeForID(ctx, messageID, cfg.Mail.Sync.Folder)
	if err != nil {
		return mailflowSummary{}, err
	}
	return runMailflowEnvelope(ctx, cfg, clients, envelope, writeBack, verifyWriteBack, deleteSource)
}

func runMailflowFirstMessage(ctx context.Context, cfg appconfig.Config, writeBack, verifyWriteBack, deleteSource bool) (mailflowSummary, bool, error) {
	clients, err := buildProviderClients(cfg)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	message, found, err := clients.Reader.FirstMessageInFolder(ctx, cfg.Mail.Sync.Folder)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	if !found {
		return mailflowSummary{}, false, nil
	}
	builder, err := buildMailflowEnvelopeBuilder(ctx, cfg, clients, deleteSource)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	envelope, err := builder.EnvelopeForMessage(ctx, message)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	summary, err := runMailflowEnvelope(ctx, cfg, clients, envelope, writeBack, verifyWriteBack, deleteSource)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	return summary, true, nil
}

func runMailflowEnvelope(ctx context.Context, cfg appconfig.Config, clients provider.Clients, envelope mailflow.MailEnvelope, writeBack, verifyWriteBack, deleteSource bool) (mailflowSummary, error) {
	coordinator, err := buildMailflowCoordinator(ctx, cfg, clients, writeBack, verifyWriteBack, deleteSource)
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
