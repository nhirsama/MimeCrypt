package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/flowruntime"
)

type resolvedMailflowTopology struct {
	flowruntime.SourceRun
	Topology appconfig.Topology
	Custom   bool
}

type resolvedMailflowRoutePlan struct {
	flowruntime.RoutePlan
}

type resolvedTopologySource struct {
	flowruntime.SourcePlan
}

type resolvedCredentialConfig struct {
	Topology       appconfig.Topology
	Credential     appconfig.Credential
	CredentialName string
	Config         appconfig.Config
	Custom         bool
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
		Topology:  plan.Topology,
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

func validateCustomCredentialFlags(cmd *cobra.Command, resolved resolvedCredentialConfig, flags ...string) error {
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

func inferSingleName(kind string, values []string) (string, error) {
	switch len(values) {
	case 0:
		return "", fmt.Errorf("topology 至少需要一个 %s", kind)
	case 1:
		return values[0], nil
	default:
		return "", fmt.Errorf("topology 存在多个 %s，请显式指定 --%s", kind, kind)
	}
}

func topologyCredentialNames(credentials map[string]appconfig.Credential) []string {
	names := make([]string, 0, len(credentials))
	for name := range credentials {
		names = append(names, strings.TrimSpace(name))
	}
	return names
}

func inferDefaultCredential(topology appconfig.Topology) (string, error) {
	if _, ok := topology.Credentials["default"]; ok {
		return "default", nil
	}
	return inferSingleName("credential", topologyCredentialNames(topology.Credentials))
}

func resolveCredentialConfig(cfg appconfig.Config, credentialFlags credentialConfigFlags) (resolvedCredentialConfig, error) {
	cfg = credentialFlags.apply(cfg)
	topologyPath := strings.TrimSpace(cfg.TopologyPath)
	if topologyPath == "" {
		if value := strings.TrimSpace(credentialFlags.credentialName); value != "" && value != "default" {
			return resolvedCredentialConfig{}, fmt.Errorf("legacy 模式只支持 credential=default")
		}
		return resolvedCredentialConfig{
			CredentialName: "default",
			Config:         cfg,
		}, nil
	}

	topology, err := appconfig.LoadTopologyFile(topologyPath)
	if err != nil {
		return resolvedCredentialConfig{}, err
	}
	if strings.TrimSpace(credentialFlags.credentialName) != "" {
		topology.DefaultCredential = strings.TrimSpace(credentialFlags.credentialName)
	}
	if strings.TrimSpace(topology.DefaultCredential) == "" {
		name, err := inferDefaultCredential(topology)
		if err != nil {
			return resolvedCredentialConfig{}, err
		}
		topology.DefaultCredential = name
	}
	credential, err := topology.DefaultCredentialConfig()
	if err != nil {
		return resolvedCredentialConfig{}, err
	}
	if err := credential.Validate(credential.Name); err != nil {
		return resolvedCredentialConfig{}, err
	}
	cfg = cfg.WithCredential(credential.Name, credential)
	return resolvedCredentialConfig{
		Topology:       topology,
		Credential:     credential,
		CredentialName: credential.Name,
		Config:         cfg,
		Custom:         true,
	}, nil
}
