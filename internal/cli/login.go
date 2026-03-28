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

	clientID := cfg.Auth.ClientID
	tenant := cfg.Auth.Tenant
	stateDir := cfg.Auth.StateDir
	authorityBaseURL := cfg.Auth.AuthorityBaseURL
	graphBaseURL := cfg.Mail.GraphBaseURL

	cmd := &cobra.Command{
		Use:   "login",
		Short: "通过 device code 登录并缓存 token",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg = syncConfig(cfg, clientID, tenant, stateDir, authorityBaseURL, graphBaseURL)

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

	cmd.Flags().StringVar(&clientID, "client-id", clientID, "Microsoft Entra 应用的 Client ID")
	cmd.Flags().StringVar(&tenant, "tenant", tenant, "租户标识，默认使用 organizations")
	cmd.Flags().StringVar(&stateDir, "state-dir", stateDir, "本地状态目录")
	cmd.Flags().StringVar(&authorityBaseURL, "authority-base-url", authorityBaseURL, "Microsoft Entra 认证基础地址")
	cmd.Flags().StringVar(&graphBaseURL, "graph-base-url", graphBaseURL, "Microsoft Graph 基础地址")

	return cmd
}
