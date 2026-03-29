package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

func newRunCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("run", "发现邮件并进行路由处理", err)
	}

	providerFlags := newProviderConfigFlags(cfg)
	processingFlags := newProcessingConfigFlags(cfg)
	syncFlags := newSyncConfigFlags(cfg)
	var once bool
	var includeExisting bool
	var debugSaveFirst bool
	var writeBack bool
	var verifyWriteBack bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "执行邮件发现、处理与回写流程",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg = providerFlags.apply(cfg, cmd)
			cfg = processingFlags.apply(cfg, cmd)
			cfg = syncFlags.apply(cfg)

			if err := validateWriteBackFlags(writeBack, verifyWriteBack, processingFlags.writeBackFolder); err != nil {
				return fmt.Errorf("run 失败: %w", err)
			}
			if err := cfg.Mail.ValidateSync(); err != nil {
				return fmt.Errorf("run 失败: %w", err)
			}
			lock, err := acquireRunLock(cfg.RunLockPath())
			if err != nil {
				return fmt.Errorf("run 失败: %w", err)
			}
			defer func() {
				_ = lock.Release()
			}()

			if debugSaveFirst {
				return runDebugSaveFirst(cmd.Context(), cfg, writeBack, verifyWriteBack)
			}

			runner, err := buildMailflowRunner(cmd.Context(), cfg, includeExisting, writeBack, verifyWriteBack, false)
			if err != nil {
				return fmt.Errorf("run 失败: %w", err)
			}

			runOnce := func() error {
				processedCount, skippedCount, deletedCount, err := runMailflowCycle(cmd.Context(), cfg, runner)
				includeExisting = false
				if err != nil {
					return err
				}
				if processedCount == 0 && skippedCount == 0 {
					fmt.Printf("本轮无待处理邮件\n")
					return nil
				}
				fmt.Printf("同步完成，本轮处理 %d 封邮件，跳过 %d 封，删除源邮件 %d 封\n", processedCount, skippedCount, deletedCount)
				return nil
			}

			if err := runOnce(); err != nil {
				return fmt.Errorf("run 失败: %w", err)
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
						fmt.Printf("本轮同步失败，下个周期继续重试: %v\n", err)
					}
				}
			}
		},
	}

	providerFlags.addFlags(cmd)
	processingFlags.addFlags(cmd)
	syncFlags.addFlags(cmd)
	cmd.Flags().BoolVar(&once, "once", false, "执行一个同步周期后退出")
	cmd.Flags().BoolVar(&includeExisting, "include-existing", false, "首次启动时也下载现有历史邮件")
	cmd.Flags().BoolVar(&debugSaveFirst, "debug-save-first", false, "调试模式下处理当前文件夹中最新的一封邮件并退出")
	cmd.Flags().BoolVar(&writeBack, "write-back", false, "处理后把邮件回写到邮箱")
	cmd.Flags().BoolVar(&verifyWriteBack, "verify-write-back", false, "回写后校验邮件是否成功写入")

	return cmd
}

func runDebugSaveFirst(ctx context.Context, cfg appconfig.Config, writeBack bool, verifyWriteBack bool) error {
	cycleCtx, cancel := context.WithTimeout(ctx, cfg.Mail.Sync.CycleTimeout)
	defer cancel()

	result, found, err := runMailflowFirstMessage(cycleCtx, cfg, writeBack, verifyWriteBack, false)
	if err != nil {
		return err
	}
	if !found {
		fmt.Printf("调试模式未找到可处理的邮件，folder=%s\n", cfg.Mail.Sync.Folder)
		return nil
	}

	fmt.Printf(
		"调试模式已处理第一封邮件，message_id=%s format=%s encrypted=%t saved_output=%t backup_path=%s path=%s bytes=%d\n",
		result.MessageID,
		result.Format,
		result.Encrypted,
		result.SavedOutput,
		result.BackupPath,
		result.Path,
		result.Bytes,
	)
	return nil
}
