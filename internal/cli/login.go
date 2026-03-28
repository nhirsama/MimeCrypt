package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

func newLoginCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("login", "通过 device code 登录并缓存 token", err)
	}

	providerFlags := newProviderConfigFlags(cfg)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "通过 device code 登录并缓存 token",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg = providerFlags.apply(cfg)

			loginCtx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
			defer cancel()

			service, err := buildLoginService(cfg)
			if err != nil {
				return fmt.Errorf("login 失败: %w", err)
			}

			result, err := service.Run(loginCtx, os.Stdout)
			if err != nil {
				return fmt.Errorf("login 失败: %w", err)
			}

			fmt.Printf("登录成功，当前账号: %s (%s)\n", result.Account, result.DisplayName)
			fmt.Printf("token 已缓存到 %s\n", result.StateDir)

			return nil
		},
	}

	providerFlags.addFlags(cmd)

	return cmd
}
