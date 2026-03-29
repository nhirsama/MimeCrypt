package cli

import (
	"context"
	"fmt"

	"mimecrypt/internal/flowruntime"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/mailflow/adapters"
)

func buildMailflowEnvelopeBuilder(ctx context.Context, resolved resolvedMailflowTopology) (*adapters.ReaderEnvelopeBuilder, error) {
	return flowruntime.BuildEnvelopeBuilder(ctx, resolved.SourceRun)
}

func runMailflowMessageByID(ctx context.Context, resolved resolvedMailflowTopology, messageID string) (mailflowSummary, error) {
	builder, err := buildMailflowEnvelopeBuilder(ctx, resolved)
	if err != nil {
		return mailflowSummary{}, err
	}
	envelope, err := builder.EnvelopeForID(ctx, messageID, resolved.Source.Folder)
	if err != nil {
		return mailflowSummary{}, err
	}
	return runMailflowEnvelope(ctx, resolved, envelope)
}

func runMailflowFirstMessage(ctx context.Context, resolved resolvedMailflowTopology) (mailflowSummary, bool, error) {
	sourceClients, err := flowruntime.BuildSourceClients(flowruntime.SourcePlan{
		Topology: resolved.Topology,
		Source:   resolved.Source,
		Config:   resolved.Config,
		Custom:   resolved.Custom,
	})
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
	builder, err := buildMailflowEnvelopeBuilder(ctx, resolved)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	envelope, err := builder.EnvelopeForMessage(ctx, message)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	summary, err := runMailflowEnvelope(ctx, resolved, envelope)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	return summary, true, nil
}

func runMailflowEnvelope(ctx context.Context, resolved resolvedMailflowTopology, envelope mailflow.MailEnvelope) (mailflowSummary, error) {
	coordinator, err := flowruntime.BuildCoordinatorWithStore(ctx, resolved.SourceRun, &mailflow.MemoryStateStore{})
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
