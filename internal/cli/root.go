package cli

import (
	"context"

	"github.com/spf13/cobra"
)

// ExecuteContext 执行整个 CLI 命令树。
func ExecuteContext(ctx context.Context) error {
	rootCmd := newRootCmd()
	return executeRootCommand(ctx, rootCmd)
}

func executeRootCommand(ctx context.Context, rootCmd *cobra.Command) error {
	err := rootCmd.ExecuteContext(ctx)
	if handleUsageError(rootCmd, err) {
		return nil
	}

	return err
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "mimecrypt",
		Short:         "MIME 邮件读取、加密与回写命令行工具",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.SetFlagErrorFunc(newFlagUsageError)

	rootCmd.AddCommand(newLoginCmd())
	rootCmd.AddCommand(newRevokeCmd())
	rootCmd.AddCommand(newHealthCmd())
	rootCmd.AddCommand(newTokenCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newEncryptCmd())
	rootCmd.AddCommand(newDownloadCmd())
	rootCmd.AddCommand(newProcessCmd())
	rootCmd.AddCommand(newRunCmd())
	localizeCobraSupport(rootCmd)

	return rootCmd
}
