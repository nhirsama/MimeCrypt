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

	clientID := cfg.Auth.ClientID
	tenant := cfg.Auth.Tenant
	stateDir := cfg.Auth.StateDir
	authorityBaseURL := cfg.Auth.AuthorityBaseURL
	graphBaseURL := cfg.Mail.GraphBaseURL
	outputDir := cfg.Mail.OutputDir
	saveOutput := cfg.Mail.SaveOutput
	backupDir := cfg.Mail.BackupDir
	backupKeyID := cfg.Mail.BackupKeyID
	auditLogPath := cfg.Mail.AuditLogPath
	writeBack := false
	verifyWriteBack := false
	writeBackFolder := cfg.Mail.WriteBackFolder

	cmd := &cobra.Command{
		Use:   "process <message-id>",
		Short: "根据邮件 ID 和配置处理邮件",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg = syncConfig(cfg, clientID, tenant, stateDir, authorityBaseURL, graphBaseURL)
			cfg.Mail.OutputDir = outputDir
			cfg.Mail.SaveOutput = saveOutput
			cfg.Mail.BackupDir = backupDir
			cfg.Mail.BackupKeyID = backupKeyID
			if cmd.Flags().Changed("audit-log-path") {
				cfg.Mail.AuditLogPath = auditLogPath
			}

			if err := validateWriteBackFlags(writeBack, verifyWriteBack, writeBackFolder); err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}
			if cfg.Mail.SaveOutput && strings.TrimSpace(cfg.Mail.OutputDir) == "" {
				return fmt.Errorf("process 失败: output-dir 不能为空")
			}
			if strings.TrimSpace(cfg.Mail.BackupDir) == "" {
				return fmt.Errorf("process 失败: backup-dir 不能为空")
			}
			if strings.TrimSpace(cfg.Mail.AuditLogPath) == "" {
				return fmt.Errorf("process 失败: audit-log-path 不能为空")
			}

			service, err := buildProcessService(cfg)
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			result, err := service.Run(cmd.Context(), buildProcessRequest(
				cfg,
				provider.MessageRef{ID: args[0]},
				writeBack,
				writeBackFolder,
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

	cmd.Flags().StringVar(&clientID, "client-id", clientID, "Microsoft Entra 应用的 Client ID")
	cmd.Flags().StringVar(&tenant, "tenant", tenant, "租户标识，默认使用 organizations")
	cmd.Flags().StringVar(&stateDir, "state-dir", stateDir, "本地状态目录")
	cmd.Flags().StringVar(&authorityBaseURL, "authority-base-url", authorityBaseURL, "Microsoft Entra 认证基础地址")
	cmd.Flags().StringVar(&graphBaseURL, "graph-base-url", graphBaseURL, "Microsoft Graph 基础地址")
	cmd.Flags().StringVar(&outputDir, "output-dir", outputDir, "处理结果输出目录")
	cmd.Flags().BoolVar(&saveOutput, "save-output", saveOutput, "是否将加密后的 PGP/MIME 额外保存到本地 output-dir，默认关闭")
	cmd.Flags().StringVar(&backupDir, "backup-dir", backupDir, "源邮件加密备份目录；保存 gpg 直接加密后的文件")
	cmd.Flags().StringVar(&backupKeyID, "backup-key-id", backupKeyID, "备份加密使用的 catch-all GPG key id；设置后所有备份统一用该 key")
	cmd.Flags().StringVar(&auditLogPath, "audit-log-path", auditLogPath, "审计日志输出路径（JSONL）")
	cmd.Flags().BoolVar(&writeBack, "write-back", writeBack, "处理后把邮件回写到邮箱")
	cmd.Flags().StringVar(&writeBackFolder, "write-back-folder", writeBackFolder, "回写目标文件夹标识；默认回写到原文件夹")
	cmd.Flags().BoolVar(&verifyWriteBack, "verify-write-back", verifyWriteBack, "回写后校验邮件是否成功写入")

	return cmd
}
