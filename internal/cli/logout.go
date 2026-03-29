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
	stateDir := cfg.Auth.StateDir

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "清理本地认证状态与缓存 token",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.Auth.StateDir = stateDir
			cfg = credentialFlags.apply(cfg)

			resolved, err := appruntime.ResolveCredentialPlan(cfg, credentialFlags.credentialName)
			if err != nil {
				return fmt.Errorf("logout 失败: %w", err)
			}
			if err := validateCustomCredentialFlags(cmd, resolved, "state-dir"); err != nil {
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

	cmd.Flags().StringVar(&stateDir, "state-dir", stateDir, "本地状态目录")
	credentialFlags.addFlags(cmd)

	return cmd
}
