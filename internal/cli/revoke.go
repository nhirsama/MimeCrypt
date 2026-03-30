package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appruntime"
)

func newRevokeCmd() *cobra.Command {
	bootstrap := loadCommandConfigBootstrap()
	credentialFlags := newCredentialConfigFlags(bootstrap.Config())
	var force bool

	cmd := &cobra.Command{
		Use:   "revoke",
		Short: "吊销 credential 会话并清理本地凭据材料",
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

			out := cmd.OutOrStdout()
			if err := service.Run(cmd.Context(), out); err != nil {
				return fmt.Errorf("revoke 失败: %w", err)
			}

			credentialName := strings.TrimSpace(resolved.EffectiveCredentialName())
			if credentialName != "" {
				_, _ = fmt.Fprintf(out, "credential=%s\n", credentialName)
			}
			_, _ = fmt.Fprintf(out, "kind=%s\n", resolved.EffectiveCredentialKind())
			if service.RequireRemote {
				_, _ = fmt.Fprintln(out, "已吊销远端会话并清除本地凭据")
			} else {
				_, _ = fmt.Fprintln(out, "已清除本地凭据")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "尽可能清除所有凭据；远端吊销失败时仍继续清理本地凭据")
	credentialFlags.addFlags(cmd)

	return cmd
}
