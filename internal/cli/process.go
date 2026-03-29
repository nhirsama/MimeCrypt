package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/flowruntime"
)

func newProcessCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("process", "根据邮件 ID 和配置处理邮件", err)
	}

	baseFlags := newBaseConfigFlags(cfg)
	topologyFlags := newTopologyConfigFlags(cfg)
	pipelineFlags := newPipelineConfigFlags(cfg)
	transactionMode := string(flowruntime.TransactionModeEphemeral)

	cmd := &cobra.Command{
		Use:   "process <message-id>",
		Short: "按邮件标识执行单封邮件处理",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg = baseFlags.apply(cfg, cmd)
			cfg = topologyFlags.apply(cfg)
			cfg = pipelineFlags.apply(cfg, cmd)

			if err := cfg.Mail.ValidatePipelineBase(); err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			resolved, err := resolveMailflowTopology(cfg, topologyFlags)
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			mode, err := parseTransactionMode(transactionMode)
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			result, err := runMailflowMessageByID(cmd.Context(), resolved, args[0], mode)
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

	baseFlags.addFlags(cmd)
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
