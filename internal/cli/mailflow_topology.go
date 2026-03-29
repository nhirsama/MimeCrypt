package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

type resolvedMailflowTopology struct {
	Topology appconfig.Topology
	Source   appconfig.Source
	Route    appconfig.Route
	Custom   bool
}

type resolvedTopologySource struct {
	Topology appconfig.Topology
	Source   appconfig.Source
	Custom   bool
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
	topologyPath := strings.TrimSpace(cfg.TopologyPath)
	if topologyPath == "" {
		topology, err := cfg.BuildTopology(legacyOptions)
		if err != nil {
			return resolvedMailflowTopology{}, err
		}
		if value := strings.TrimSpace(topologyFlags.routeName); value != "" && value != topology.DefaultRoute {
			return resolvedMailflowTopology{}, fmt.Errorf("legacy 模式只支持 route=%s", topology.DefaultRoute)
		}
		if value := strings.TrimSpace(topologyFlags.sourceName); value != "" && value != topology.DefaultSource {
			return resolvedMailflowTopology{}, fmt.Errorf("legacy 模式只支持 source=%s", topology.DefaultSource)
		}
		source, err := topology.DefaultSourceConfig()
		if err != nil {
			return resolvedMailflowTopology{}, err
		}
		route, err := topology.DefaultRouteConfig()
		if err != nil {
			return resolvedMailflowTopology{}, err
		}
		return resolvedMailflowTopology{
			Topology: topology,
			Source:   source,
			Route:    route,
		}, nil
	}

	topology, err := appconfig.LoadTopologyFile(topologyPath)
	if err != nil {
		return resolvedMailflowTopology{}, err
	}
	if strings.TrimSpace(topologyFlags.routeName) != "" {
		topology.DefaultRoute = strings.TrimSpace(topologyFlags.routeName)
	}
	if strings.TrimSpace(topology.DefaultRoute) == "" {
		name, err := inferSingleName("route", topologyRouteNames(topology.Routes))
		if err != nil {
			return resolvedMailflowTopology{}, err
		}
		topology.DefaultRoute = name
	}

	route, ok := topology.Routes[topology.DefaultRoute]
	if !ok {
		return resolvedMailflowTopology{}, fmt.Errorf("topology route 不存在: %s", topology.DefaultRoute)
	}
	if strings.TrimSpace(topologyFlags.sourceName) != "" {
		topology.DefaultSource = strings.TrimSpace(topologyFlags.sourceName)
	}
	if strings.TrimSpace(topology.DefaultSource) == "" {
		name, err := inferDefaultSourceForRoute(route)
		if err != nil {
			return resolvedMailflowTopology{}, err
		}
		topology.DefaultSource = name
	}

	source, ok := topology.Sources[topology.DefaultSource]
	if !ok {
		return resolvedMailflowTopology{}, fmt.Errorf("topology source 不存在: %s", topology.DefaultSource)
	}
	if !routeContainsSource(route, source.Name) {
		return resolvedMailflowTopology{}, fmt.Errorf("route %s 不包含 source %s", route.Name, source.Name)
	}

	if strings.TrimSpace(source.StatePath) == "" {
		source.StatePath = cfg.Mail.FlowProducerStatePathFor(source.Name, source.Driver, source.Folder)
		topology.Sources[source.Name] = source
	}
	if strings.TrimSpace(route.StateDir) == "" {
		route.StateDir = cfg.Mail.FlowStateDirFor(route.Name, source.Name, source.Driver, source.Folder)
		topology.Routes[route.Name] = route
	}
	if err := topology.Validate(); err != nil {
		return resolvedMailflowTopology{}, err
	}
	return resolvedMailflowTopology{
		Topology: topology,
		Source:   source,
		Route:    route,
		Custom:   true,
	}, nil
}

func validateCustomTopologyFlags(cmd *cobra.Command, topology resolvedMailflowTopology, flags ...string) error {
	if !topology.Custom || cmd == nil {
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
	if value := strings.TrimSpace(topologyFlags.routeName); value != "" {
		return resolvedTopologySource{}, fmt.Errorf("该命令不支持 route 选择: %s", value)
	}
	topologyPath := strings.TrimSpace(cfg.TopologyPath)
	if topologyPath == "" {
		topology, err := cfg.BuildTopology(appconfig.TopologyOptions{})
		if err != nil {
			return resolvedTopologySource{}, err
		}
		if value := strings.TrimSpace(topologyFlags.sourceName); value != "" && value != topology.DefaultSource {
			return resolvedTopologySource{}, fmt.Errorf("legacy 模式只支持 source=%s", topology.DefaultSource)
		}
		source, err := topology.DefaultSourceConfig()
		if err != nil {
			return resolvedTopologySource{}, err
		}
		return resolvedTopologySource{
			Topology: topology,
			Source:   source,
		}, nil
	}

	topology, err := appconfig.LoadTopologyFile(topologyPath)
	if err != nil {
		return resolvedTopologySource{}, err
	}
	if strings.TrimSpace(topologyFlags.sourceName) != "" {
		topology.DefaultSource = strings.TrimSpace(topologyFlags.sourceName)
	}
	if strings.TrimSpace(topology.DefaultSource) == "" {
		name, err := inferSingleName("source", topologySourceNames(topology.Sources))
		if err != nil {
			return resolvedTopologySource{}, err
		}
		topology.DefaultSource = name
	}
	source, ok := topology.Sources[topology.DefaultSource]
	if !ok {
		return resolvedTopologySource{}, fmt.Errorf("topology source 不存在: %s", topology.DefaultSource)
	}
	if strings.TrimSpace(source.StatePath) == "" {
		source.StatePath = cfg.Mail.FlowProducerStatePathFor(source.Name, source.Driver, source.Folder)
		topology.Sources[source.Name] = source
	}
	if err := topology.Validate(); err != nil {
		return resolvedTopologySource{}, err
	}
	return resolvedTopologySource{
		Topology: topology,
		Source:   source,
		Custom:   true,
	}, nil
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

func inferDefaultSourceForRoute(route appconfig.Route) (string, error) {
	sourceRefs := make([]string, 0, len(route.SourceRefs))
	for _, ref := range route.SourceRefs {
		if value := strings.TrimSpace(ref); value != "" {
			sourceRefs = append(sourceRefs, value)
		}
	}
	return inferSingleName("source", sourceRefs)
}

func routeContainsSource(route appconfig.Route, sourceName string) bool {
	sourceName = strings.TrimSpace(sourceName)
	for _, ref := range route.SourceRefs {
		if strings.TrimSpace(ref) == sourceName {
			return true
		}
	}
	return false
}

func topologyRouteNames(routes map[string]appconfig.Route) []string {
	names := make([]string, 0, len(routes))
	for name := range routes {
		names = append(names, strings.TrimSpace(name))
	}
	return names
}

func topologySourceNames(sources map[string]appconfig.Source) []string {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, strings.TrimSpace(name))
	}
	return names
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
