package cli

import (
	"fmt"
	"mimecrypt/internal/flowruntime"
	"strings"

	"mimecrypt/internal/appconfig"
)

type resolvedMailflowTopology struct {
	flowruntime.SourceRun
}

type resolvedMailflowRoutePlan struct {
	flowruntime.RoutePlan
}

type resolvedTopologySource struct {
	flowruntime.SourcePlan
}

func resolveMailflowTopology(cfg appconfig.Config, topologyFlags topologyConfigFlags) (resolvedMailflowTopology, error) {
	cfg = topologyFlags.apply(cfg)
	plan, err := flowruntime.ResolveRoutePlan(cfg, flowruntime.Selector{
		RouteName:  strings.TrimSpace(topologyFlags.routeName),
		SourceName: strings.TrimSpace(topologyFlags.sourceName),
	}, flowruntime.RoutePlanSingleSource)
	if err != nil {
		return resolvedMailflowTopology{}, err
	}
	if len(plan.Runs) != 1 {
		return resolvedMailflowTopology{}, fmt.Errorf("mailflow 单源解析结果无效: runs=%d", len(plan.Runs))
	}
	return resolvedMailflowTopology{
		SourceRun: plan.Runs[0],
	}, nil
}

func resolveMailflowRoutePlan(cfg appconfig.Config, topologyFlags topologyConfigFlags) (resolvedMailflowRoutePlan, error) {
	cfg = topologyFlags.apply(cfg)
	plan, err := flowruntime.ResolveRoutePlan(cfg, flowruntime.Selector{
		RouteName:  strings.TrimSpace(topologyFlags.routeName),
		SourceName: strings.TrimSpace(topologyFlags.sourceName),
	}, flowruntime.RoutePlanAllSources)
	if err != nil {
		return resolvedMailflowRoutePlan{}, err
	}
	return resolvedMailflowRoutePlan{RoutePlan: plan}, nil
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
