package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/appruntime"
)

func newRevokeCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("revoke", "吊销远端登录状态并清除本地凭据", err)
	}

	credentialFlags := newCredentialConfigFlags(cfg)
	var force bool

	cmd := &cobra.Command{
		Use:   "revoke",
		Short: "吊销远端登录状态并清除本地凭据",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg = credentialFlags.apply(cfg)

			resolved, err := appruntime.ResolveCredentialPlan(cfg, credentialFlags.credentialName)
			if err != nil {
				return fmt.Errorf("revoke 失败: %w", err)
			}
			cfg = resolved.Config

			service, err := appruntime.BuildRevokeService(cfg, force)
			if err != nil {
				return fmt.Errorf("revoke 失败: %w", err)
			}

			if err := service.Run(cmd.Context()); err != nil {
				return fmt.Errorf("revoke 失败: %w", err)
			}

			fmt.Printf("已吊销远端登录状态并清除本地凭据\n")
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "尽可能清除所有凭据；远端吊销失败时仍继续清理本地凭据")
	credentialFlags.addFlags(cmd)

	return cmd
}
