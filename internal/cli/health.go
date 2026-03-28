package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/modules/health"
)

func newHealthCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("health", "检查运行环境、认证状态和 provider 连通性", err)
	}

	providerFlags := newProviderConfigFlags(cfg)
	syncFlags := newSyncConfigFlags(cfg)
	timeout := 30 * time.Second

	cmd := &cobra.Command{
		Use:   "health",
		Short: "检查运行环境、认证状态和 provider 连通性",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg = providerFlags.apply(cfg, cmd)
			cfg = syncFlags.apply(cfg)

			service, err := buildHealthService(cfg)
			if err != nil {
				return fmt.Errorf("health 失败: %w", err)
			}

			healthCtx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()

			result, err := service.Run(healthCtx)
			if err != nil {
				return fmt.Errorf("health 失败: %w", err)
			}

			fmt.Println(health.FormatText(result))
			if !result.OK() {
				return fmt.Errorf("health 检查失败")
			}
			return nil
		},
	}

	providerFlags.addFlags(cmd)
	syncFlags.addFlags(cmd)
	cmd.Flags().DurationVar(&timeout, "timeout", timeout, "健康检查总超时时间")

	return cmd
}
