package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

func newProcessCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("process", "根据邮件 ID 和配置处理邮件", err)
	}

	providerFlags := newProviderConfigFlags(cfg)
	processingFlags := newProcessingConfigFlags(cfg)
	folder := cfg.Mail.Sync.Folder
	writeBack := false
	verifyWriteBack := false

	cmd := &cobra.Command{
		Use:   "process <message-id>",
		Short: "根据邮件 ID 和配置处理邮件",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg = providerFlags.apply(cfg, cmd)
			cfg = processingFlags.apply(cfg, cmd)
			cfg.Mail.Sync.Folder = folder

			if err := validateWriteBackFlags(writeBack, verifyWriteBack, processingFlags.writeBackFolder); err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}
			if cfg.Mail.Pipeline.SaveOutput && strings.TrimSpace(cfg.Mail.Pipeline.OutputDir) == "" {
				return fmt.Errorf("process 失败: output-dir 不能为空")
			}
			if strings.TrimSpace(cfg.Mail.Pipeline.BackupDir) == "" {
				return fmt.Errorf("process 失败: backup-dir 不能为空")
			}
			if !cfg.Mail.Pipeline.HasAuditOutput() {
				return fmt.Errorf("process 失败: 至少需要一个审计输出：audit-log-path 或 audit-stdout")
			}

			service, err := buildProcessService(cfg)
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			result, err := service.Run(cmd.Context(), buildProcessRequest(
				cfg,
				provider.MessageRef{ID: args[0], FolderID: folder},
				writeBack,
				processingFlags.writeBackFolder,
				verifyWriteBack,
			))
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			fmt.Printf(
				"处理完成，message_id=%s format=%s encrypted=%t already_encrypted=%t saved_output=%t backup_path=%s wrote_back=%t verified=%t path=%s bytes=%d\n",
				result.MessageID,
				result.Format,
				result.Encrypted,
				result.AlreadyEncrypted,
				result.SavedOutput,
				result.BackupPath,
				result.WroteBack,
				result.Verified,
				result.Path,
				result.Bytes,
			)
			return nil
		},
	}

	providerFlags.addFlags(cmd)
	processingFlags.addFlags(cmd)
	cmd.Flags().StringVar(&folder, "folder", folder, "邮件所在文件夹；Graph 用 folder id，IMAP 用 mailbox 名称")
	cmd.Flags().BoolVar(&writeBack, "write-back", writeBack, "处理后把邮件回写到邮箱")
	cmd.Flags().BoolVar(&verifyWriteBack, "verify-write-back", verifyWriteBack, "回写后校验邮件是否成功写入")

	return cmd
}
