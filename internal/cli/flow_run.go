package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/mailflow"
)

func newFlowRunCmd() *cobra.Command {
	return newMailflowLoopCmd(mailflowLoopCmdOptions{
		use:              "flow-run",
		short:            "基于 mailflow 执行邮件级事务处理",
		errorPrefix:      "flow-run",
		includeDebug:     false,
		hidden:           true,
		deprecatedNotice: "请改用 run",
	})
}

func runMailflowCycle(ctx context.Context, cycleTimeout time.Duration, runner interface {
	RunOnce(context.Context) (mailflow.Result, bool, error)
}) (int, int, int, error) {
	cycleCtx, cancel := context.WithTimeout(ctx, cycleTimeout)
	defer cancel()

	processedCount := 0
	skippedCount := 0
	deletedCount := 0
	for {
		result, processed, err := runner.RunOnce(cycleCtx)
		if err != nil {
			return processedCount, skippedCount, deletedCount, err
		}
		if !processed {
			return processedCount, skippedCount, deletedCount, nil
		}
		if result.Skipped {
			skippedCount++
		} else {
			processedCount++
		}
		if result.SourceDeleted {
			deletedCount++
		}
	}
}
