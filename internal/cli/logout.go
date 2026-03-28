package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

func newLogoutCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("logout", "清除本地登录状态", err)
	}

	stateDir := cfg.Auth.StateDir

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "清除本地登录状态",
		Args:  noArgs(),
		RunE: func(*cobra.Command, []string) error {
			cfg.Auth.StateDir = stateDir
			service := buildLogoutService(cfg)

			if err := service.Run(); err != nil {
				return fmt.Errorf("logout 失败: %w", err)
			}

			fmt.Printf("已清除本地登录状态: %s\n", cfg.Auth.TokenPath())
			return nil
		},
	}

	cmd.Flags().StringVar(&stateDir, "state-dir", stateDir, "本地状态目录")

	return cmd
}
