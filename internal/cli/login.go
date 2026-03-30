package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/appruntime"
	"mimecrypt/internal/interact"
	"mimecrypt/internal/providers"
)

func newLoginCmd() *cobra.Command {
	bootstrap := loadCommandConfigBootstrap()
	credentialFlags := newCredentialConfigFlags(bootstrap.Config())

	cmd := &cobra.Command{
		Use:   "login",
		Short: "交互式配置 credential 并写入本地登录状态",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := bootstrap.Error(); err != nil {
				return fmt.Errorf("login 失败: %w", err)
			}
			cfg := credentialFlags.apply(bootstrap.Config())

			resolved, err := appruntime.ResolveCredentialCommandPlan(cfg, credentialFlags.credentialName)
			if err != nil {
				return fmt.Errorf("login 失败: %w", err)
			}
			cfg = resolved.Config

			localCfg, err := appconfig.LoadLocalConfig(cfg.Auth.StateDir)
			if err != nil {
				return fmt.Errorf("login 失败: %w", err)
			}
			out := cmd.OutOrStdout()
			configureDrivers := loginDriversForConfig(resolved)
			localCfg, cfg, resolvedDrivers, err := providers.ConfigureLoginLocalConfig(cfg, localCfg, cmd.InOrStdin(), out, configureDrivers...)
			if err != nil {
				if errors.Is(err, interact.ErrAbort) {
					_, _ = fmt.Fprintln(out, "已取消 credential 登录配置")
					return nil
				}
				return fmt.Errorf("login 失败: %w", err)
			}
			if err := appconfig.SaveLocalConfig(cfg.Auth.StateDir, localCfg); err != nil {
				return fmt.Errorf("login 失败: 保存本地驱动配置失败: %w", err)
			}
			resolved.Config = cfg
			resolved.AuthDrivers = resolvedDrivers

			loginCtx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
			defer cancel()

			service, err := appruntime.BuildLoginService(resolved)
			if err != nil {
				return fmt.Errorf("login 失败: %w", err)
			}

			result, err := service.Run(loginCtx, out)
			if err != nil {
				return fmt.Errorf("login 失败: %w", err)
			}
			_, _ = fmt.Fprintf(out, "登录成功，账号: %s (%s)\n", result.Account, result.DisplayName)
			if strings.TrimSpace(resolved.CredentialName) != "" {
				_, _ = fmt.Fprintf(out, "credential=%s\n", resolved.CredentialName)
			}
			printCredentialBindingSummary(out, resolved)
			_, _ = fmt.Fprintf(out, "token 已缓存到 %s\n", result.StateDir)

			return nil
		},
	}

	credentialFlags.addFlags(cmd)

	return cmd
}

func printCredentialBindingSummary(out io.Writer, plan appruntime.CredentialPlan) {
	if out == nil {
		out = io.Discard
	}
	if len(plan.AuthDrivers) > 0 {
		drivers := append([]string(nil), plan.AuthDrivers...)
		slices.Sort(drivers)
		_, _ = fmt.Fprintf(out, "drivers=%s\n", strings.Join(drivers, ","))
	}
	if sources := plan.BindingNames(appruntime.CredentialBindingSource); len(sources) > 0 {
		_, _ = fmt.Fprintf(out, "sources=%s\n", strings.Join(sources, ","))
	}
	if sinks := plan.BindingNames(appruntime.CredentialBindingSink); len(sinks) > 0 {
		_, _ = fmt.Fprintf(out, "sinks=%s\n", strings.Join(sinks, ","))
	}
}

func loginDriversForConfig(plan appruntime.CredentialPlan) []string {
	if len(plan.Bindings) == 0 {
		return nil
	}
	return append([]string(nil), plan.AuthDrivers...)
}
