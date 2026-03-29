package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/appruntime"
)

func newLogoutCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("logout", "清除本地登录状态", err)
	}

	credentialFlags := newCredentialConfigFlags(cfg)

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "清理本地认证状态与缓存 token",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg = credentialFlags.apply(cfg)

			resolved, err := appruntime.ResolveCredentialPlan(cfg, credentialFlags.credentialName)
			if err != nil {
				return fmt.Errorf("logout 失败: %w", err)
			}
			cfg = resolved.Config
			service, err := appruntime.BuildLogoutService(cfg)
			if err != nil {
				return fmt.Errorf("logout 失败: %w", err)
			}

			if err := service.Run(); err != nil {
				return fmt.Errorf("logout 失败: %w", err)
			}

			fmt.Printf("已清除本地登录状态\n")
			return nil
		},
	}

	credentialFlags.addFlags(cmd)

	return cmd
}
