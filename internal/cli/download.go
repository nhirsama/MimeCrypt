package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/flowruntime"
)

func newDownloadCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("download", "按邮件 ID 下载原始 MIME", err)
	}

	baseFlags := newBaseConfigFlags(cfg)
	topologyFlags := newTopologyConfigFlags(cfg)
	outputDir := cfg.Mail.Pipeline.OutputDir

	cmd := &cobra.Command{
		Use:   "download <message-id>",
		Short: "按邮件标识下载原始 MIME",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg = baseFlags.apply(cfg, cmd)
			cfg = topologyFlags.apply(cfg)
			cfg.Mail.Pipeline.OutputDir = outputDir

			if cfg.Mail.Pipeline.OutputDir == "" {
				return fmt.Errorf("download 失败: output-dir 不能为空")
			}

			resolved, err := resolveTopologySource(cfg, topologyFlags)
			if err != nil {
				return fmt.Errorf("download 失败: %w", err)
			}
			service, err := flowruntime.BuildDownloadService(resolved.SourcePlan)
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

	baseFlags.addFlags(cmd)
	topologyFlags.addSourceFlags(cmd)
	cmd.Flags().StringVar(&outputDir, "output-dir", outputDir, "MIME 文件输出目录")

	return cmd
}
