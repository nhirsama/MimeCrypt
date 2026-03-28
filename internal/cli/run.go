package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/modules/discover"
	"mimecrypt/internal/provider"
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
		Short: "发现邮件并进行路由处理",
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

			service, err := buildDiscoverService(cfg)
			if err != nil {
				return fmt.Errorf("run 失败: %w", err)
			}

			if debugSaveFirst {
				return runDebugSaveFirst(cmd.Context(), cfg, service, writeBack, processingFlags.writeBackFolder, verifyWriteBack)
			}

			runOnce := func() error {
				err := runDiscoverCycle(cmd.Context(), cfg, service, includeExisting, writeBack, processingFlags.writeBackFolder, verifyWriteBack)
				includeExisting = false
				return err
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
	cmd.Flags().BoolVar(&once, "once", false, "只执行一次同步后退出")
	cmd.Flags().BoolVar(&includeExisting, "include-existing", false, "首次启动时也下载现有历史邮件")
	cmd.Flags().BoolVar(&debugSaveFirst, "debug-save-first", false, "调试模式：直接处理当前文件夹中最新的一封邮件并退出")
	cmd.Flags().BoolVar(&writeBack, "write-back", false, "处理后把邮件回写到邮箱")
	cmd.Flags().BoolVar(&verifyWriteBack, "verify-write-back", false, "回写后校验邮件是否成功写入")

	return cmd
}

func runDebugSaveFirst(ctx context.Context, cfg appconfig.Config, service *discover.Service, writeBack bool, writeBackFolder string, verifyWriteBack bool) error {
	cycleCtx, cancel := context.WithTimeout(ctx, cfg.Mail.Sync.CycleTimeout)
	defer cancel()

	result, err := service.DebugFirst(cycleCtx, discover.Request{
		Folder:  cfg.Mail.Sync.Folder,
		Process: buildProcessRequest(cfg, provider.MessageRef{}, writeBack, writeBackFolder, verifyWriteBack),
	})
	if err != nil {
		return err
	}
	if !result.Found {
		fmt.Printf("调试模式未找到可处理的邮件，folder=%s\n", cfg.Mail.Sync.Folder)
		return nil
	}

	fmt.Printf(
		"调试模式已处理第一封邮件，message_id=%s format=%s encrypted=%t saved_output=%t backup_path=%s path=%s bytes=%d\n",
		result.Process.MessageID,
		result.Process.Format,
		result.Process.Encrypted,
		result.Process.SavedOutput,
		result.Process.BackupPath,
		result.Process.Path,
		result.Process.Bytes,
	)
	return nil
}

func runDiscoverCycle(ctx context.Context, cfg appconfig.Config, service *discover.Service, includeExisting, writeBack bool, writeBackFolder string, verifyWriteBack bool) error {
	cycleCtx, cancel := context.WithTimeout(ctx, cfg.Mail.Sync.CycleTimeout)
	defer cancel()

	result, err := service.RunCycle(cycleCtx, discover.Request{
		Folder:          cfg.Mail.Sync.Folder,
		StatePath:       cfg.Mail.SyncStatePath(),
		IncludeExisting: includeExisting,
		Process:         buildProcessRequest(cfg, provider.MessageRef{}, writeBack, writeBackFolder, verifyWriteBack),
	})
	if err != nil {
		return err
	}

	if result.Bootstrapped && !includeExisting {
		fmt.Printf("首次同步已建立基线，跳过了 %d 封现有邮件\n", result.Skipped)
	} else {
		fmt.Printf("同步完成，本轮处理 %d 封邮件\n", result.Processed)
	}

	return nil
}
