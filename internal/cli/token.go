package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/appruntime"
)

func newTokenCmd() *cobra.Command {
	bootstrap := loadCommandConfigBootstrap()
	cfg := bootstrap.Config()

	root := &cobra.Command{
		Use:   "token",
		Short: "查询或导入本地 token 状态",
	}
	if err := bootstrap.Error(); err != nil {
		root.PersistentPreRunE = func(*cobra.Command, []string) error {
			return fmt.Errorf("token 失败: %w", err)
		}
	}
	root.AddCommand(newTokenStatusCmd(cfg))
	root.AddCommand(newTokenImportCmd(cfg))
	return root
}

func newTokenStatusCmd(cfg appconfig.Config) *cobra.Command {
	credentialFlags := newCredentialConfigFlags(cfg)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "查询本地 token 状态",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg = credentialFlags.apply(cfg)

			resolved, err := appruntime.ResolveCredentialCommandPlan(cfg, credentialFlags.credentialName)
			if err != nil {
				return fmt.Errorf("token status 失败: %w", err)
			}
			cfg = resolved.Config

			service, err := appruntime.BuildTokenStateService(cfg)
			if err != nil {
				return fmt.Errorf("token status 失败: %w", err)
			}

			result, err := service.Status()
			if err != nil {
				return fmt.Errorf("token status 失败: %w", err)
			}

			if !result.Present {
				if strings.TrimSpace(resolved.CredentialName) != "" {
					fmt.Printf("token_absent credential=%s state_dir=%s token_store=%s\n", resolved.CredentialName, result.StateDir, result.TokenStore)
					return nil
				}
				fmt.Printf("token_absent state_dir=%s token_store=%s\n", result.StateDir, result.TokenStore)
				return nil
			}

			expiresAt := ""
			if !result.Token.ExpiresAt.IsZero() {
				expiresAt = result.Token.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
			}
			if strings.TrimSpace(resolved.CredentialName) != "" {
				fmt.Printf(
					"token_present credential=%s state_dir=%s token_store=%s expires_at=%s scope=%q has_refresh_token=%t\n",
					resolved.CredentialName,
					result.StateDir,
					result.TokenStore,
					expiresAt,
					result.Token.Scope,
					strings.TrimSpace(result.Token.RefreshToken) != "",
				)
				return nil
			}
			fmt.Printf(
				"token_present state_dir=%s token_store=%s expires_at=%s scope=%q has_refresh_token=%t\n",
				result.StateDir,
				result.TokenStore,
				expiresAt,
				result.Token.Scope,
				strings.TrimSpace(result.Token.RefreshToken) != "",
			)
			return nil
		},
	}
	credentialFlags.addFlags(cmd)
	return cmd
}

func newTokenImportCmd(cfg appconfig.Config) *cobra.Command {
	credentialFlags := newCredentialConfigFlags(cfg)

	cmd := &cobra.Command{
		Use:   "import [path|-]",
		Short: "从 JSON 文件或标准输入导入 token",
		Args:  argRange(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg = credentialFlags.apply(cfg)

			resolved, err := appruntime.ResolveCredentialCommandPlan(cfg, credentialFlags.credentialName)
			if err != nil {
				return fmt.Errorf("token import 失败: %w", err)
			}
			cfg = resolved.Config

			service, err := appruntime.BuildTokenStateService(cfg)
			if err != nil {
				return fmt.Errorf("token import 失败: %w", err)
			}

			src, closeFn, err := openTokenImportSource(args)
			if err != nil {
				return fmt.Errorf("token import 失败: %w", err)
			}
			defer closeFn()

			result, err := service.Import(src)
			if err != nil {
				return fmt.Errorf("token import 失败: %w", err)
			}

			if strings.TrimSpace(resolved.CredentialName) != "" {
				fmt.Printf(
					"已导入 token，credential=%s state_dir=%s token_store=%s has_refresh_token=%t\n",
					resolved.CredentialName,
					result.StateDir,
					result.TokenStore,
					strings.TrimSpace(result.Token.RefreshToken) != "",
				)
				return nil
			}
			fmt.Printf(
				"已导入 token，state_dir=%s token_store=%s has_refresh_token=%t\n",
				result.StateDir,
				result.TokenStore,
				strings.TrimSpace(result.Token.RefreshToken) != "",
			)
			return nil
		},
	}

	credentialFlags.addFlags(cmd)
	return cmd
}

func openTokenImportSource(args []string) (*os.File, func(), error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "-" {
		return os.Stdin, func() {}, nil
	}
	file, err := os.Open(strings.TrimSpace(args[0]))
	if err != nil {
		return nil, func() {}, err
	}
	return file, func() { _ = file.Close() }, nil
}
