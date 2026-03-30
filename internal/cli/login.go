package cli

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/appruntime"
)

func newLoginCmd() *cobra.Command {
	bootstrap := loadCommandConfigBootstrap()
	credentialFlags := newCredentialConfigFlags(bootstrap.Config())

	cmd := &cobra.Command{
		Use:   "login [imap-username]",
		Short: "为 credential 创建设备登录状态并写入本地 token 缓存",
		Args:  argRange(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bootstrap.Error(); err != nil {
				return fmt.Errorf("login 失败: %w", err)
			}
			cfg := credentialFlags.apply(bootstrap.Config())

			resolved, err := appruntime.ResolveCredentialPlan(cfg, credentialFlags.credentialName)
			if err != nil {
				return fmt.Errorf("login 失败: %w", err)
			}
			cfg = resolved.Config
			cfg = applyLoginIMAPUsernameArg(cfg, args)
			resolved.Config = cfg

			loginCtx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
			defer cancel()

			service, err := appruntime.BuildLoginService(resolved)
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
			if strings.TrimSpace(resolved.CredentialName) != "" {
				fmt.Printf("credential=%s\n", resolved.CredentialName)
			}
			printCredentialBindingSummary(resolved)
			fmt.Printf("token 已缓存到 %s\n", result.StateDir)

			return nil
		},
	}

	credentialFlags.addFlags(cmd)

	return cmd
}

func printCredentialBindingSummary(plan appruntime.CredentialPlan) {
	if len(plan.AuthDrivers) > 0 {
		drivers := append([]string(nil), plan.AuthDrivers...)
		slices.Sort(drivers)
		fmt.Printf("drivers=%s\n", strings.Join(drivers, ","))
	}
	if sources := plan.BindingNames(appruntime.CredentialBindingSource); len(sources) > 0 {
		fmt.Printf("sources=%s\n", strings.Join(sources, ","))
	}
	if sinks := plan.BindingNames(appruntime.CredentialBindingSink); len(sinks) > 0 {
		fmt.Printf("sinks=%s\n", strings.Join(sinks, ","))
	}
}

func applyLoginIMAPUsernameArg(cfg appconfig.Config, args []string) appconfig.Config {
	if len(args) != 1 {
		return cfg
	}
	if os.Getenv("MIMECRYPT_IMAP_USERNAME") != "" {
		return cfg
	}
	cfg.Mail.Client.IMAPUsername = strings.TrimSpace(args[0])
	return cfg
}
