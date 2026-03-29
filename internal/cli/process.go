package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

func newProcessCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("process", "根据邮件 ID 和配置处理邮件", err)
	}

	providerFlags := newProviderConfigFlags(cfg)
	topologyFlags := newTopologyConfigFlags(cfg)
	processingFlags := newProcessingConfigFlags(cfg)
	folder := cfg.Mail.Sync.Folder
	writeBack := false
	verifyWriteBack := false

	cmd := &cobra.Command{
		Use:   "process <message-id>",
		Short: "按邮件标识执行单封邮件处理",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg = providerFlags.apply(cfg, cmd)
			cfg = topologyFlags.apply(cfg)
			cfg = processingFlags.apply(cfg, cmd)
			cfg.Mail.Sync.Folder = folder

			resolved, err := resolveMailflowTopology(cfg, topologyFlags, appconfig.TopologyOptions{
				WriteBack:       writeBack,
				VerifyWriteBack: verifyWriteBack,
			})
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}
			if resolved.Custom {
				if err := validateCustomTopologyFlags(cmd, resolved,
					"save-output",
					"output-dir",
					"write-back",
					"verify-write-back",
					"write-back-provider",
					"write-back-folder",
					"folder",
				); err != nil {
					return fmt.Errorf("process 失败: %w", err)
				}
				if err := cfg.Mail.ValidatePipelineBase(); err != nil {
					return fmt.Errorf("process 失败: %w", err)
				}
			} else {
				if err := validateMailflowFlags(cfg.Mail.Pipeline.SaveOutput, writeBack, verifyWriteBack, false, processingFlags.writeBackFolder); err != nil {
					return fmt.Errorf("process 失败: %w", err)
				}
				if err := cfg.Mail.ValidateSync(); err != nil {
					return fmt.Errorf("process 失败: %w", err)
				}
			}

			result, err := runMailflowMessageByID(cmd.Context(), cfg, resolved, args[0])
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
	topologyFlags.addFlags(cmd)
	processingFlags.addFlags(cmd)
	cmd.Flags().StringVar(&folder, "folder", folder, "邮件所在文件夹；Graph 用 folder id，IMAP 用 mailbox 名称")
	cmd.Flags().BoolVar(&writeBack, "write-back", writeBack, "处理完成后回写邮件至邮箱")
	cmd.Flags().BoolVar(&verifyWriteBack, "verify-write-back", verifyWriteBack, "回写后校验邮件是否成功写入")

	return cmd
}
