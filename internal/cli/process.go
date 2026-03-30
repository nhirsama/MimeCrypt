package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/flowruntime"
)

func newProcessCmd() *cobra.Command {
	bootstrap := loadCommandConfigBootstrap()
	topologyFlags := newTopologyConfigFlags(bootstrap.Config())
	pipelineFlags := newPipelineConfigFlags(bootstrap.Config())
	transactionMode := string(flowruntime.TransactionModeEphemeral)

	cmd := &cobra.Command{
		Use:   "process <message-id>",
		Short: "按邮件标识执行单封邮件处理",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bootstrap.Error(); err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}
			cfg := topologyFlags.apply(bootstrap.Config())
			cfg = pipelineFlags.apply(cfg, cmd)

			if err := cfg.Mail.ValidatePipelineBase(); err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			resolved, err := flowruntime.ResolveSingleSourceRun(cfg, flowruntime.Selector{
				RouteName:  strings.TrimSpace(topologyFlags.routeName),
				SourceName: strings.TrimSpace(topologyFlags.sourceName),
			})
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			mode, err := parseTransactionMode(transactionMode)
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			runner, err := flowruntime.BuildSingleMessageRunner(cmd.Context(), resolved, mode)
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}
			result, err := runner.RunMessageByID(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}
			summary, err := summarizeSingleMessageResult(result)
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			fmt.Printf(
				"处理完成，message_id=%s format=%s encrypted=%t already_encrypted=%t saved_output=%t backup_path=%s wrote_back=%t verified=%t path=%s bytes=%d\n",
				summary.MessageID,
				summary.Format,
				summary.Encrypted,
				summary.AlreadyEncrypted,
				summary.SavedOutput,
				summary.BackupPath,
				summary.WroteBack,
				summary.Verified,
				summary.Path,
				summary.Bytes,
			)
			return nil
		},
	}

	topologyFlags.addFlags(cmd)
	pipelineFlags.addFlags(cmd)
	cmd.Flags().StringVar(&transactionMode, "transaction-mode", transactionMode, "单封处理事务状态模式：ephemeral 或 persistent")

	return cmd
}

func parseTransactionMode(value string) (flowruntime.TransactionMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(flowruntime.TransactionModeEphemeral):
		return flowruntime.TransactionModeEphemeral, nil
	case string(flowruntime.TransactionModePersistent):
		return flowruntime.TransactionModePersistent, nil
	default:
		return "", fmt.Errorf("不支持的 transaction mode: %s", value)
	}
}
