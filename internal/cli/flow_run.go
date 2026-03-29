package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
)

func newFlowRunCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("flow-run", "基于 mailflow 执行邮件级事务处理", err)
	}

	providerFlags := newProviderConfigFlags(cfg)
	processingFlags := newProcessingConfigFlags(cfg)
	syncFlags := newSyncConfigFlags(cfg)
	var once bool
	var includeExisting bool
	var writeBack bool
	var verifyWriteBack bool
	var deleteSource bool

	cmd := &cobra.Command{
		Use:   "flow-run",
		Short: "基于 mailflow 执行邮件级事务处理",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg = providerFlags.apply(cfg, cmd)
			cfg = processingFlags.apply(cfg, cmd)
			cfg = syncFlags.apply(cfg)

			if err := validateMailflowFlags(cfg.Mail.Pipeline.SaveOutput, writeBack, verifyWriteBack, deleteSource, processingFlags.writeBackFolder); err != nil {
				return fmt.Errorf("flow-run 失败: %w", err)
			}
			if err := cfg.Mail.ValidateSync(); err != nil {
				return fmt.Errorf("flow-run 失败: %w", err)
			}

			lock, err := acquireRunLock(cfg.RunLockPath())
			if err != nil {
				return fmt.Errorf("flow-run 失败: %w", err)
			}
			defer func() {
				_ = lock.Release()
			}()

			runner, err := buildMailflowRunner(cmd.Context(), cfg, includeExisting, writeBack, verifyWriteBack, deleteSource)
			if err != nil {
				return fmt.Errorf("flow-run 失败: %w", err)
			}

			runOnce := func() error {
				processedCount, skippedCount, deletedCount, err := runMailflowCycle(cmd.Context(), cfg, runner)
				includeExisting = false
				if err != nil {
					return err
				}
				if processedCount == 0 && skippedCount == 0 {
					fmt.Printf("mailflow 本轮无待处理邮件\n")
					return nil
				}
				fmt.Printf("mailflow 同步完成，本轮处理 %d 封邮件，跳过 %d 封，删除源邮件 %d 封\n", processedCount, skippedCount, deletedCount)
				return nil
			}

			if err := runOnce(); err != nil {
				return fmt.Errorf("flow-run 失败: %w", err)
			}
			if once {
				return nil
			}

			ticker := time.NewTicker(cfg.Mail.Sync.PollInterval)
			defer ticker.Stop()

			for {
				select {
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				case <-ticker.C:
					if err := runOnce(); err != nil {
						fmt.Printf("mailflow 本轮同步失败，下个周期继续重试: %v\n", err)
					}
				}
			}
		},
	}

	providerFlags.addFlags(cmd)
	processingFlags.addFlags(cmd)
	syncFlags.addFlags(cmd)
	cmd.Flags().BoolVar(&once, "once", false, "执行一个 mailflow 同步周期后退出")
	cmd.Flags().BoolVar(&includeExisting, "include-existing", false, "首次启动时也纳入当前已有邮件")
	cmd.Flags().BoolVar(&writeBack, "write-back", false, "将处理后的邮件写入邮箱出口")
	cmd.Flags().BoolVar(&verifyWriteBack, "verify-write-back", false, "写入邮箱出口后校验邮件是否成功写入")
	cmd.Flags().BoolVar(&deleteSource, "delete-source", false, "当写入目标与来源属于同一逻辑邮箱存储时删除源邮件")

	return cmd
}

func runMailflowCycle(ctx context.Context, cfg appconfig.Config, runner interface {
	RunOnce(context.Context) (mailflow.Result, bool, error)
}) (int, int, int, error) {
	cycleCtx, cancel := context.WithTimeout(ctx, cfg.Mail.Sync.CycleTimeout)
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
