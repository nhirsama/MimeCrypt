package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
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
		Use:   "login [imap-username]",
		Short: "执行 device code 登录并写入本地 token 缓存",
		Args:  argRange(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg = providerFlags.apply(cfg, cmd)
			cfg = applyLoginIMAPUsernameArg(cfg, cmd, args)

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
			if username := strings.TrimSpace(cfg.Mail.Client.IMAPUsername); username != "" {
				if err := appconfig.SaveLocalConfig(cfg.Auth.StateDir, appconfig.LocalConfig{IMAPUsername: username}); err != nil {
					return fmt.Errorf("login 成功，但保存 IMAP 用户名失败: %w", err)
				}
			}

			fmt.Printf("登录成功，账号: %s (%s)\n", result.Account, result.DisplayName)
			fmt.Printf("token 已缓存到 %s\n", result.StateDir)

			return nil
		},
	}

	providerFlags.addFlags(cmd)

	return cmd
}

func applyLoginIMAPUsernameArg(cfg appconfig.Config, cmd *cobra.Command, args []string) appconfig.Config {
	if len(args) != 1 {
		return cfg
	}
	if os.Getenv("MIMECRYPT_IMAP_USERNAME") != "" {
		return cfg
	}
	if cmd != nil && cmd.Flags().Changed("imap-username") {
		return cfg
	}
	cfg.Mail.Client.IMAPUsername = strings.TrimSpace(args[0])
	return cfg
}
