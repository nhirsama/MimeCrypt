package cli

import (
	"context"

	"github.com/spf13/cobra"
)

// ExecuteContext 执行整个 CLI 命令树。
func ExecuteContext(ctx context.Context) error {
	rootCmd := newRootCmd()
	return rootCmd.ExecuteContext(ctx)
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "mimecrypt",
		Short:        "登录、抓取、处理并回写 MIME 邮件的 CLI 工具",
		SilenceUsage: true,
	}

	rootCmd.AddCommand(newLoginCmd())
	rootCmd.AddCommand(newLogoutCmd())
	rootCmd.AddCommand(newDownloadCmd())
	rootCmd.AddCommand(newProcessCmd())
	rootCmd.AddCommand(newRunCmd())

	return rootCmd
}
