package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
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
		Short: "交互式配置 credential 运行时并建立设备凭据",
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

			localCfg := resolved.LocalConfig
			out := cmd.OutOrStdout()
			localCfg, cfg, _, err = providers.ConfigureLoginLocalConfig(
				resolved.EffectiveCredentialKind(),
				cfg,
				localCfg,
				cmd.InOrStdin(),
				out,
				resolved.SuggestedAuthHints()...,
			)
			if err != nil {
				if errors.Is(err, interact.ErrAbort) {
					_, _ = fmt.Fprintln(out, "已取消 credential 登录配置")
					return nil
				}
				return fmt.Errorf("login 失败: %w", err)
			}
			localCfg = localCfg.Normalize()
			if err := appconfig.SaveLocalConfig(cfg.Auth.StateDir, localCfg); err != nil {
				return fmt.Errorf("login 失败: 保存 credential 本地配置失败: %w", err)
			}
			resolved.Config = cfg
			resolved.LocalConfig = localCfg

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
			credentialName := firstNonEmpty(strings.TrimSpace(result.Credential), strings.TrimSpace(resolved.CredentialName))
			if credentialName != "" {
				_, _ = fmt.Fprintf(out, "credential=%s\n", credentialName)
			}
			if result.Kind != "" {
				_, _ = fmt.Fprintf(out, "kind=%s\n", result.Kind)
			}
			if result.Runtime != "" {
				_, _ = fmt.Fprintf(out, "runtime=%s\n", result.Runtime)
			}
			if result.AuthProfile != "" {
				_, _ = fmt.Fprintf(out, "auth_profile=%s\n", result.AuthProfile)
			}
			if result.Account != "" || result.DisplayName != "" {
				_, _ = fmt.Fprintf(out, "登录成功，账号: %s (%s)\n", result.Account, result.DisplayName)
			} else {
				_, _ = fmt.Fprintln(out, "登录成功")
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
	if sources := plan.BindingNames(appruntime.CredentialBindingSource); len(sources) > 0 {
		_, _ = fmt.Fprintf(out, "sources=%s\n", strings.Join(sources, ","))
	}
	if sinks := plan.BindingNames(appruntime.CredentialBindingSink); len(sinks) > 0 {
		_, _ = fmt.Fprintf(out, "sinks=%s\n", strings.Join(sinks, ","))
	}
}
