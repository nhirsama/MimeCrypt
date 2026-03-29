package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/appruntime"
	"mimecrypt/internal/flowruntime"
)

type resolvedMailflowTopology struct {
	flowruntime.SourceRun
	Custom bool
}

type resolvedMailflowRoutePlan struct {
	flowruntime.RoutePlan
}

type resolvedTopologySource struct {
	flowruntime.SourcePlan
}

func resolveMailflowTopology(cfg appconfig.Config, topologyFlags topologyConfigFlags, legacyOptions appconfig.TopologyOptions) (resolvedMailflowTopology, error) {
	cfg = topologyFlags.apply(cfg)
	plan, err := flowruntime.ResolveRoutePlan(cfg, flowruntime.Selector{
		RouteName:  strings.TrimSpace(topologyFlags.routeName),
		SourceName: strings.TrimSpace(topologyFlags.sourceName),
	}, legacyOptions, flowruntime.RoutePlanSingleSource)
	if err != nil {
		return resolvedMailflowTopology{}, err
	}
	if len(plan.Runs) != 1 {
		return resolvedMailflowTopology{}, fmt.Errorf("mailflow 单源解析结果无效: runs=%d", len(plan.Runs))
	}
	return resolvedMailflowTopology{
		SourceRun: plan.Runs[0],
		Custom:    plan.Custom,
	}, nil
}

func resolveMailflowRoutePlan(cfg appconfig.Config, topologyFlags topologyConfigFlags, legacyOptions appconfig.TopologyOptions) (resolvedMailflowRoutePlan, error) {
	cfg = topologyFlags.apply(cfg)
	plan, err := flowruntime.ResolveRoutePlan(cfg, flowruntime.Selector{
		RouteName:  strings.TrimSpace(topologyFlags.routeName),
		SourceName: strings.TrimSpace(topologyFlags.sourceName),
	}, legacyOptions, flowruntime.RoutePlanAllSources)
	if err != nil {
		return resolvedMailflowRoutePlan{}, err
	}
	return resolvedMailflowRoutePlan{RoutePlan: plan}, nil
}

func validateCustomTopologyFlags(cmd *cobra.Command, custom bool, flags ...string) error {
	if !custom || cmd == nil {
		return nil
	}
	for _, name := range flags {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--%s 与 --topology-file 不能同时覆盖同一类路由配置", name)
		}
	}
	return nil
}

func validateCustomCredentialFlags(cmd *cobra.Command, resolved appruntime.CredentialPlan, flags ...string) error {
	if !resolved.Custom || cmd == nil {
		return nil
	}
	for _, name := range flags {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--%s 与 --credential/--topology-file 不能同时覆盖同一类认证配置", name)
		}
	}
	return nil
}

func resolveTopologySource(cfg appconfig.Config, topologyFlags topologyConfigFlags) (resolvedTopologySource, error) {
	cfg = topologyFlags.apply(cfg)
	plan, err := flowruntime.ResolveSourcePlan(cfg, flowruntime.Selector{
		RouteName:  strings.TrimSpace(topologyFlags.routeName),
		SourceName: strings.TrimSpace(topologyFlags.sourceName),
	})
	if err != nil {
		return resolvedTopologySource{}, err
	}
	return resolvedTopologySource{SourcePlan: plan}, nil
}
