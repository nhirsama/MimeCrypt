package cli

import (
	"fmt"
	"os"
	"slices"
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
		Short: "查询或导入 credential token 状态",
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

			service, err := appruntime.BuildTokenStateService(resolved)
			if err != nil {
				return fmt.Errorf("token status 失败: %w", err)
			}

			result, err := service.Status()
			if err != nil {
				return fmt.Errorf("token status 失败: %w", err)
			}
			out := cmd.OutOrStdout()
			meta := formatTokenMeta(tokenMeta{
				Credential:     result.Credential,
				CredentialKind: result.CredentialKind,
				Runtime:        result.Runtime,
				Drivers:        result.Drivers,
				StateDir:       result.StateDir,
				TokenStore:     result.TokenStore,
			})

			if !result.Present {
				_, _ = fmt.Fprintf(out, "token_absent%s\n", meta)
				return nil
			}

			expiresAt := ""
			if !result.Token.ExpiresAt.IsZero() {
				expiresAt = result.Token.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
			}
			_, _ = fmt.Fprintf(
				out,
				"token_present%s expires_at=%s scope=%q has_refresh_token=%t\n",
				meta,
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

			service, err := appruntime.BuildTokenStateService(resolved)
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
			out := cmd.OutOrStdout()
			meta := formatTokenMeta(tokenMeta{
				Credential:     result.Credential,
				CredentialKind: result.CredentialKind,
				Runtime:        result.Runtime,
				Drivers:        result.Drivers,
				StateDir:       result.StateDir,
				TokenStore:     result.TokenStore,
			})

			_, _ = fmt.Fprintf(
				out,
				"已导入 token%s has_refresh_token=%t\n",
				meta,
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

type tokenMeta struct {
	Credential     string
	CredentialKind string
	Runtime        string
	Drivers        []string
	StateDir       string
	TokenStore     string
}

func formatTokenMeta(result tokenMeta) string {
	parts := make([]string, 0, 6)
	if credential := strings.TrimSpace(result.Credential); credential != "" {
		parts = append(parts, "credential="+credential)
	}
	if kind := strings.TrimSpace(result.CredentialKind); kind != "" {
		parts = append(parts, "kind="+kind)
	}
	if runtime := strings.TrimSpace(result.Runtime); runtime != "" {
		parts = append(parts, "runtime="+runtime)
	}
	if len(result.Drivers) > 0 {
		drivers := append([]string(nil), result.Drivers...)
		slices.Sort(drivers)
		parts = append(parts, "drivers="+strings.Join(drivers, ","))
	}
	parts = append(parts, "state_dir="+result.StateDir)
	parts = append(parts, "token_store="+result.TokenStore)
	return " " + strings.Join(parts, " ")
}
