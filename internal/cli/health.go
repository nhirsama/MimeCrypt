package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/flowruntime"
	"mimecrypt/internal/modules/health"
)

func newHealthCmd() *cobra.Command {
	bootstrap := loadCommandConfigBootstrap()
	topologyFlags := newTopologyConfigFlags(bootstrap.Config())
	timeout := 30 * time.Second
	deep := false

	cmd := &cobra.Command{
		Use:   "health",
		Short: "检查运行环境、缓存凭据与可选连通性状态",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := bootstrap.Error(); err != nil {
				return fmt.Errorf("health 失败: %w", err)
			}
			cfg := topologyFlags.apply(bootstrap.Config())

			resolved, err := flowruntime.ResolveRoutePlan(cfg, flowruntime.Selector{
				RouteName:  topologyFlags.routeName,
				SourceName: topologyFlags.sourceName,
			}, flowruntime.RoutePlanAllSources)
			if err != nil {
				return fmt.Errorf("health 失败: %w", err)
			}
			healthCtx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			return runRouteHealth(healthCtx, resolved, deep)
		},
	}

	topologyFlags.addFlags(cmd)
	cmd.Flags().BoolVar(&deep, "deep", deep, "执行包含 token 刷新与 provider 连通性探测的深度检查")
	cmd.Flags().DurationVar(&timeout, "timeout", timeout, "健康检查总超时时间")

	return cmd
}

func runRouteHealth(ctx context.Context, plan flowruntime.RoutePlan, deep bool) error {
	failures := false
	for idx, run := range plan.Runs {
		service, err := flowruntime.BuildHealthService(ctx, run)
		if err != nil {
			return err
		}
		service.Deep = deep

		result, err := service.Run(ctx)
		if err != nil {
			return err
		}
		if len(plan.Runs) > 1 {
			if idx > 0 {
				fmt.Println()
			}
			fmt.Printf("[source=%s]\n", run.Source.Name)
		}
		fmt.Println(health.FormatText(result))
		if !result.OK() {
			failures = true
		}
	}
	if failures {
		return fmt.Errorf("health 检查失败")
	}
	return nil
}
