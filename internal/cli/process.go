package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/modules/process"
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
	writeBack := false
	verifyWriteBack := false

	cmd := &cobra.Command{
		Use:   "process <message-id>",
		Short: "根据邮件 ID 和配置处理邮件",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg = syncConfig(cfg, clientID, tenant, stateDir, authorityBaseURL, graphBaseURL)
			cfg.Mail.OutputDir = outputDir

			if err := validateWriteBackFlags(writeBack, verifyWriteBack); err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}
			if strings.TrimSpace(cfg.Mail.OutputDir) == "" {
				return fmt.Errorf("process 失败: output-dir 不能为空")
			}

			service, err := buildProcessService(cfg)
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			result, err := service.Run(cmd.Context(), process.Request{
				MessageID:       args[0],
				OutputDir:       cfg.Mail.OutputDir,
				WriteBack:       writeBack,
				VerifyWriteBack: verifyWriteBack,
			})
			if err != nil {
				return fmt.Errorf("process 失败: %w", err)
			}

			fmt.Printf(
				"处理完成，message_id=%s format=%s encrypted=%t already_encrypted=%t wrote_back=%t verified=%t path=%s bytes=%d\n",
				result.MessageID,
				result.Format,
				result.Encrypted,
				result.AlreadyEncrypted,
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
	cmd.Flags().BoolVar(&writeBack, "write-back", writeBack, "处理后把邮件回写到邮箱")
	cmd.Flags().BoolVar(&verifyWriteBack, "verify-write-back", verifyWriteBack, "回写后校验邮件是否成功写入")

	return cmd
}
