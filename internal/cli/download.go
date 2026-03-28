package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

func newDownloadCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("download", "按邮件 ID 下载原始 MIME", err)
	}

	providerFlags := newProviderConfigFlags(cfg)
	outputDir := cfg.Mail.Pipeline.OutputDir
	folder := cfg.Mail.Sync.Folder

	cmd := &cobra.Command{
		Use:   "download <message-id>",
		Short: "按邮件 ID 下载原始 MIME",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg = providerFlags.apply(cfg)
			cfg.Mail.Pipeline.OutputDir = outputDir
			cfg.Mail.Sync.Folder = folder

			if strings.TrimSpace(cfg.Mail.Pipeline.OutputDir) == "" {
				return fmt.Errorf("download 失败: output-dir 不能为空")
			}

			service, err := buildDownloadService(cfg)
			if err != nil {
				return fmt.Errorf("download 失败: %w", err)
			}

			result, err := service.Save(cmd.Context(), args[0], cfg.Mail.Pipeline.OutputDir)
			if err != nil {
				return fmt.Errorf("download 失败: %w", err)
			}

			fmt.Printf("已保存邮件 MIME，message_id=%s subject=%q path=%s bytes=%d\n", result.Message.ID, result.Message.Subject, result.Path, result.Bytes)
			return nil
		},
	}

	providerFlags.addFlags(cmd)
	cmd.Flags().StringVar(&outputDir, "output-dir", outputDir, "MIME 文件输出目录")
	cmd.Flags().StringVar(&folder, "folder", folder, "邮件所在文件夹；Graph 用 folder id，IMAP 用 mailbox 名称")

	return cmd
}
