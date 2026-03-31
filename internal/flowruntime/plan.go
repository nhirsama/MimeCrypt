package flowruntime

import (
	"fmt"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers"
)

type RoutePlanMode string

const (
	RoutePlanSingleSource RoutePlanMode = "single-source"
	RoutePlanAllSources   RoutePlanMode = "all-sources"
)

type Selector struct {
	SourceName string
	RouteName  string
}

type SourcePlan struct {
	Topology appconfig.Topology
	Source   appconfig.Source
	Config   appconfig.Config
}

type SinkPlan struct {
	Sink    appconfig.Sink
	Config  appconfig.Config
	Mailbox string
}

type SourceRun struct {
	Source                appconfig.Source
	Route                 appconfig.Route
	RuntimeTargets        []appconfig.RouteTarget
	ExecutionPlan         mailflow.ExecutionPlan
	Config                appconfig.Config
	Sinks                 map[string]SinkPlan
	SourceDeleteSemantics provider.DeleteSemantics
	LockPath              string
}

type RoutePlan struct {
	Topology appconfig.Topology
	Route    appconfig.Route
	Runs     []SourceRun
}

func ResolveSourcePlan(cfg appconfig.Config, selector Selector) (SourcePlan, error) {
	if value := strings.TrimSpace(selector.RouteName); value != "" {
		return SourcePlan{}, fmt.Errorf("该命令不支持 route 选择: %s", value)
	}

	topology, err := loadRuntimeTopology(cfg)
	if err != nil {
		return SourcePlan{}, err
	}
	sourceName, err := selectSourceName(topology, selector.SourceName)
	if err != nil {
		return SourcePlan{}, err
	}
	topology.DefaultSource = sourceName

	source, err := topology.SourceInstance(sourceName)
	if err != nil {
		return SourcePlan{}, fmt.Errorf("topology source 不存在: %s", sourceName)
	}
	sourceCfg, err := configForSource(cfg, topology, source)
	if err != nil {
		return SourcePlan{}, err
	}
	source, err = sourceWithRuntimeStatePath(sourceCfg, source)
	if err != nil {
		return SourcePlan{}, err
	}
	return SourcePlan{
		Topology: topology,
		Source:   source,
		Config:   sourceCfg,
	}, nil
}

func ResolveSingleSourceRun(cfg appconfig.Config, selector Selector) (SourceRun, error) {
	plan, err := ResolveRoutePlan(cfg, selector, RoutePlanSingleSource)
	if err != nil {
		return SourceRun{}, err
	}
	if len(plan.Runs) != 1 {
		return SourceRun{}, fmt.Errorf("mailflow 单源解析结果无效: runs=%d", len(plan.Runs))
	}
	return plan.Runs[0], nil
}

func ResolveRoutePlan(cfg appconfig.Config, selector Selector, mode RoutePlanMode) (RoutePlan, error) {
	topology, err := loadRuntimeTopology(cfg)
	if err != nil {
		return RoutePlan{}, err
	}
	routeName, err := selectRouteName(topology, selector.RouteName)
	if err != nil {
		return RoutePlan{}, err
	}
	topology.DefaultRoute = routeName

	route, err := topology.RouteInstance(routeName)
	if err != nil {
		return RoutePlan{}, fmt.Errorf("topology route 不存在: %s", routeName)
	}
	sourceNames, err := selectRouteSources(topology, route, selector.SourceName, mode)
	if err != nil {
		return RoutePlan{}, err
	}
	if strings.TrimSpace(topology.DefaultSource) == "" && len(sourceNames) > 0 {
		topology.DefaultSource = sourceNames[0]
	}

	runs := make([]SourceRun, 0, len(sourceNames))
	for _, sourceName := range sourceNames {
		source, err := topology.SourceInstance(sourceName)
		if err != nil {
			return RoutePlan{}, fmt.Errorf("topology source 不存在: %s", sourceName)
		}
		run, err := buildSourceRun(cfg, topology, route, source)
		if err != nil {
			return RoutePlan{}, err
		}
		runs = append(runs, run)
	}

	return RoutePlan{
		Topology: topology,
		Route:    route,
		Runs:     runs,
	}, nil
}

func loadRuntimeTopology(cfg appconfig.Config) (appconfig.Topology, error) {
	topologyPath := strings.TrimSpace(cfg.TopologyPath)
	if topologyPath == "" {
		return appconfig.Topology{}, fmt.Errorf("topology path 未配置")
	}
	topology, err := appconfig.LoadTopologyFile(topologyPath)
	if err != nil {
		return appconfig.Topology{}, err
	}
	if err := topology.ValidateStructure(); err != nil {
		return appconfig.Topology{}, err
	}
	if err := validateConfiguredDrivers(topology); err != nil {
		return appconfig.Topology{}, err
	}
	return topology, nil
}

func validateConfiguredDrivers(topology appconfig.Topology) error {
	for _, source := range topology.Sources {
		if err := providers.ValidateSourceConfig(source); err != nil {
			return err
		}
	}
	for _, sink := range topology.Sinks {
		if err := providers.ValidateSinkConfig(sink); err != nil {
			return err
		}
	}
	return nil
}

func selectRouteSources(topology appconfig.Topology, route appconfig.Route, sourceName string, mode RoutePlanMode) ([]string, error) {
	if value := strings.TrimSpace(sourceName); value != "" {
		if !routeContainsSource(route, value) {
			return nil, fmt.Errorf("route %s 不包含 source %s", route.Name, value)
		}
		return []string{value}, nil
	}

	if mode == RoutePlanAllSources {
		names := make([]string, 0, len(route.SourceRefs))
		seen := make(map[string]struct{}, len(route.SourceRefs))
		for _, ref := range route.SourceRefs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			if _, exists := seen[ref]; exists {
				continue
			}
			seen[ref] = struct{}{}
			names = append(names, ref)
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("route %s 至少需要一个 source", route.Name)
		}
		return names, nil
	}

	defaultSource := strings.TrimSpace(topology.DefaultSource)
	if defaultSource != "" {
		if !routeContainsSource(route, defaultSource) {
			return nil, fmt.Errorf("route %s 不包含 source %s", route.Name, defaultSource)
		}
		return []string{defaultSource}, nil
	}

	name, err := inferDefaultSourceForRoute(route)
	if err != nil {
		return nil, err
	}
	return []string{name}, nil
}

func buildSourceRun(cfg appconfig.Config, topology appconfig.Topology, route appconfig.Route, source appconfig.Source) (SourceRun, error) {
	sourceCfg, err := configForSource(cfg, topology, source)
	if err != nil {
		return SourceRun{}, err
	}
	source, err = sourceWithRuntimeStatePath(sourceCfg, source)
	if err != nil {
		return SourceRun{}, err
	}

	runRoute := runtimeRouteForSource(sourceCfg, route, source)
	runtimeTargets := compileRuntimeTargets(route.Targets)
	executionPlan, err := buildMailflowPlan(runRoute, runtimeTargets)
	if err != nil {
		return SourceRun{}, err
	}

	sinks := make(map[string]SinkPlan)
	for _, target := range runtimeTargets {
		sinkRef := strings.TrimSpace(target.SinkRef)
		if sinkRef == "" {
			continue
		}
		if _, exists := sinks[sinkRef]; exists {
			continue
		}
		sink, ok := topology.Sinks[sinkRef]
		if !ok {
			return SourceRun{}, fmt.Errorf("route %s 引用了不存在的 sink: %s", runRoute.Name, sinkRef)
		}
		sinkCfg, err := configForSink(cfg, topology, sink)
		if err != nil {
			return SourceRun{}, err
		}
		sinks[sinkRef] = SinkPlan{
			Sink:    sink,
			Config:  sinkCfg,
			Mailbox: sinkMailbox(source, sink),
		}
	}
	return SourceRun{
		Source:                source,
		Route:                 runRoute,
		RuntimeTargets:        runtimeTargets,
		ExecutionPlan:         executionPlan,
		Config:                sourceCfg,
		Sinks:                 sinks,
		SourceDeleteSemantics: sourceDeleteSemantics(source.Driver),
		LockPath:              sourceCfg.RunLockPathFor(source.Name, source.Driver, source.Folder),
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

func selectSourceName(topology appconfig.Topology, explicit string) (string, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(topology.DefaultSource); value != "" {
		return value, nil
	}
	return inferSingleName("source", topologySourceNames(topology.Sources))
}

func selectRouteName(topology appconfig.Topology, explicit string) (string, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(topology.DefaultRoute); value != "" {
		return value, nil
	}
	return inferSingleName("route", topologyRouteNames(topology.Routes))
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

func configForSource(cfg appconfig.Config, topology appconfig.Topology, source appconfig.Source) (appconfig.Config, error) {
	sourceSpec, ok := providers.LookupSourceSpec(source.Driver)
	if !ok {
		return appconfig.Config{}, fmt.Errorf("source %s 不支持 driver: %s", source.Name, source.Driver)
	}
	if !sourceSpec.RequiresCredential {
		return cfg, nil
	}
	return applyTopologyCredential(cfg, topology, source.CredentialRef)
}

func configForSink(cfg appconfig.Config, topology appconfig.Topology, sink appconfig.Sink) (appconfig.Config, error) {
	sinkSpec, ok := providers.LookupSinkSpec(sink.Driver)
	if !ok {
		return appconfig.Config{}, fmt.Errorf("sink %s 不支持 driver: %s", sink.Name, sink.Driver)
	}
	if !sinkSpec.RequiresCredential {
		return cfg, nil
	}
	sinkCfg, err := applyTopologyCredential(cfg, topology, sink.CredentialRef)
	if err != nil {
		return appconfig.Config{}, err
	}
	return sinkCfg, nil
}

func sinkMailbox(source appconfig.Source, sink appconfig.Sink) string {
	if mailbox := strings.TrimSpace(sink.Folder); mailbox != "" {
		return mailbox
	}
	return strings.TrimSpace(source.Folder)
}

func applyTopologyCredential(cfg appconfig.Config, topology appconfig.Topology, credentialRef string) (appconfig.Config, error) {
	resolvedRef, err := topology.ResolveCredentialRef(credentialRef)
	if err != nil {
		return appconfig.Config{}, err
	}
	if resolvedRef == "" {
		return cfg, nil
	}
	credential, err := topology.CredentialConfig(resolvedRef)
	if err != nil {
		return appconfig.Config{}, err
	}
	return cfg.WithCredential(credential.Name, credential), nil
}

func sourceWithRuntimeStatePath(cfg appconfig.Config, source appconfig.Source) (appconfig.Source, error) {
	source = source.Configured()
	sourceSpec, ok := providers.LookupSourceSpec(source.Driver)
	if !ok {
		return appconfig.Source{}, fmt.Errorf("source %s 不支持 driver: %s", source.Name, source.Driver)
	}
	modeSpec, ok := sourceSpec.ModeSpec(source.Mode)
	if !ok {
		return appconfig.Source{}, fmt.Errorf("source %s 的 driver %s 不支持 mode: %s", source.Name, source.Driver, source.Mode)
	}
	if !modeSpec.RequiresStatePath {
		return source, nil
	}
	return source.WithRuntimeStatePath(cfg.Mail.FlowProducerStatePathFor(source.Name, source.Driver, source.Folder)), nil
}

func runtimeRouteForSource(cfg appconfig.Config, route appconfig.Route, source appconfig.Source) appconfig.Route {
	route = route.Configured()
	return route.WithRuntimeStateDir(cfg.Mail.FlowStateDirFor(route.Name, source.Name, source.Driver, source.Folder))
}

func compileRuntimeTargets(targets []appconfig.RouteTarget) []appconfig.RouteTarget {
	return append([]appconfig.RouteTarget(nil), targets...)
}

func normalizeDriver(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return strings.ToLower(strings.TrimSpace(fallback))
	}
	return value
}
