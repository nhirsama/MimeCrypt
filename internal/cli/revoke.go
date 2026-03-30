package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appruntime"
)

func newRevokeCmd() *cobra.Command {
	bootstrap := loadCommandConfigBootstrap()
	credentialFlags := newCredentialConfigFlags(bootstrap.Config())
	var force bool

	cmd := &cobra.Command{
		Use:   "revoke",
		Short: "撤销 credential 登录状态并清理本地凭据",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := bootstrap.Error(); err != nil {
				return fmt.Errorf("revoke 失败: %w", err)
			}
			cfg := credentialFlags.apply(bootstrap.Config())

			resolved, err := appruntime.ResolveCredentialCommandPlan(cfg, credentialFlags.credentialName)
			if err != nil {
				return fmt.Errorf("revoke 失败: %w", err)
			}

			service, err := appruntime.BuildRevokeService(resolved, force)
			if err != nil {
				return fmt.Errorf("revoke 失败: %w", err)
			}

			if err := service.Run(cmd.Context(), os.Stdout); err != nil {
				return fmt.Errorf("revoke 失败: %w", err)
			}

			if service.RequireRemote {
				fmt.Printf("已吊销远端登录状态并清除本地凭据\n")
			} else {
				fmt.Printf("已清除本地凭据\n")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "尽可能清除所有凭据；远端吊销失败时仍继续清理本地凭据")
	credentialFlags.addFlags(cmd)

	return cmd
}
