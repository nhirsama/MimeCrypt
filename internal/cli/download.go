package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/flowruntime"
)

func newDownloadCmd() *cobra.Command {
	bootstrap := loadCommandConfigBootstrap()
	topologyFlags := newTopologyConfigFlags(bootstrap.Config())
	outputDir := bootstrap.Config().Mail.Pipeline.OutputDir

	cmd := &cobra.Command{
		Use:   "download <message-id>",
		Short: "按邮件标识下载原始 MIME",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bootstrap.Error(); err != nil {
				return fmt.Errorf("download 失败: %w", err)
			}
			cfg := topologyFlags.apply(bootstrap.Config())
			cfg.Mail.Pipeline.OutputDir = outputDir

			if cfg.Mail.Pipeline.OutputDir == "" {
				return fmt.Errorf("download 失败: output-dir 不能为空")
			}

			resolved, err := flowruntime.ResolveSourcePlan(cfg, flowruntime.Selector{
				RouteName:  strings.TrimSpace(topologyFlags.routeName),
				SourceName: strings.TrimSpace(topologyFlags.sourceName),
			})
			if err != nil {
				return fmt.Errorf("download 失败: %w", err)
			}
			service, err := flowruntime.BuildDownloadService(resolved)
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

	topologyFlags.addSourceFlags(cmd)
	cmd.Flags().StringVar(&outputDir, "output-dir", outputDir, "MIME 文件输出目录")

	return cmd
}
