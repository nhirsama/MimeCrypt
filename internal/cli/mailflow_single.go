package cli

import (
	"context"
	"fmt"

	"mimecrypt/internal/flowruntime"
	"mimecrypt/internal/mailflow"
)

func buildMailflowSingleRunner(ctx context.Context, resolved resolvedMailflowTopology) (*flowruntime.SingleMessageRunner, error) {
	return buildMailflowSingleRunnerWithMode(ctx, resolved, flowruntime.TransactionModeEphemeral)
}

func buildMailflowSingleRunnerWithMode(ctx context.Context, resolved resolvedMailflowTopology, mode flowruntime.TransactionMode) (*flowruntime.SingleMessageRunner, error) {
	return flowruntime.BuildSingleMessageRunner(ctx, resolved.SourceRun, mode)
}

func runMailflowMessageByID(ctx context.Context, resolved resolvedMailflowTopology, messageID string, mode flowruntime.TransactionMode) (mailflowSummary, error) {
	runner, err := buildMailflowSingleRunnerWithMode(ctx, resolved, mode)
	if err != nil {
		return mailflowSummary{}, err
	}
	result, err := runner.RunMessageByID(ctx, messageID)
	if err != nil {
		return mailflowSummary{}, err
	}
	return summarizeSingleMessageResult(result)
}

func runMailflowFirstMessage(ctx context.Context, resolved resolvedMailflowTopology) (mailflowSummary, bool, error) {
	runner, err := buildMailflowSingleRunner(ctx, resolved)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	result, found, err := runner.RunFirstMessage(ctx)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	if !found {
		return mailflowSummary{}, false, nil
	}
	summary, err := summarizeSingleMessageResult(result)
	if err != nil {
		return mailflowSummary{}, false, err
	}
	return summary, true, nil
}

func summarizeSingleMessageResult(result mailflow.Result) (mailflowSummary, error) {
	summary, err := summarizeMailflowResult(result)
	if err != nil {
		return mailflowSummary{}, err
	}
	if summary.MessageID == "" {
		return mailflowSummary{}, fmt.Errorf("mailflow 结果缺少 message id")
	}
	return summary, nil
}
