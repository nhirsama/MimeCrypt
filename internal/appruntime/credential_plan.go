package appruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/providers"
)

const implicitDefaultCredentialName = "default"

type CredentialBindingKind string

const (
	CredentialBindingSource CredentialBindingKind = "source"
	CredentialBindingSink   CredentialBindingKind = "sink"
)

type CredentialBinding struct {
	Kind     CredentialBindingKind
	Name     string
	Driver   string
	Implicit bool
}

type CredentialPlan struct {
	Topology       appconfig.Topology
	Credential     appconfig.Credential
	CredentialName string
	Config         appconfig.Config
	Bindings       []CredentialBinding
	AuthDrivers    []string
}

// ResolveCredentialCommandPlan 为 login/revoke/token 这类 credential 生命周期命令解析配置。
// 这些命令优先管理 credential 自身；topology 仅用于补充 overlay、绑定摘要与最小 scopes。
func ResolveCredentialCommandPlan(cfg appconfig.Config, explicit string) (CredentialPlan, error) {
	plan, err := resolveBootstrapCredentialPlanForCommand(cfg, explicit)
	if err != nil {
		return CredentialPlan{}, err
	}

	topologyPath := strings.TrimSpace(cfg.TopologyPath)
	if topologyPath == "" {
		return plan, nil
	}

	topology, err := appconfig.LoadTopologyFile(topologyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && isDefaultCredentialTopologyPath(cfg, topologyPath) {
			return plan, nil
		}
		return plan, nil
	}

	credentialName, err := resolveCredentialRefForCommand(topology, explicit)
	if err != nil {
		return CredentialPlan{}, err
	}
	if credentialName == "" {
		return plan, nil
	}

	credential, err := topology.CredentialConfig(credentialName)
	if err != nil {
		return CredentialPlan{}, err
	}

	plan.Topology = topology
	plan.Credential = credential
	plan.CredentialName = credential.Name
	plan.Config = cfg.WithCredential(credential.Name, credential)
	localCfg := appconfig.LocalConfig{}
	plan.Config, localCfg, err = applyLocalConfigOverlay(plan.Config)
	if err != nil {
		return CredentialPlan{}, err
	}
	plan.Bindings, plan.AuthDrivers = resolveCredentialBindingsForCommand(topology, credential.Name)
	if len(plan.AuthDrivers) == 0 {
		plan.AuthDrivers = append([]string(nil), localCfg.Drivers...)
	}
	return plan, nil
}

func ResolveCredentialPlan(cfg appconfig.Config, explicit string) (CredentialPlan, error) {
	topologyPath := strings.TrimSpace(cfg.TopologyPath)
	if topologyPath == "" {
		if strings.TrimSpace(explicit) != "" {
			return CredentialPlan{}, fmt.Errorf("credential %s 需要 topology 配置", strings.TrimSpace(explicit))
		}
		return resolveBootstrapCredentialPlan(cfg)
	}

	topology, err := appconfig.LoadTopologyFile(topologyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && isDefaultCredentialTopologyPath(cfg, topologyPath) {
			if strings.TrimSpace(explicit) != "" {
				return CredentialPlan{}, fmt.Errorf("credential %s 需要 topology 配置", strings.TrimSpace(explicit))
			}
			return resolveBootstrapCredentialPlan(cfg)
		}
		return CredentialPlan{}, err
	}
	credentialName, err := topology.ResolveCredentialRef(explicit)
	if err != nil {
		return CredentialPlan{}, err
	}

	plan := CredentialPlan{
		Topology: topology,
		Config:   cfg,
	}
	if credentialName == "" {
		plan.Config, _, err = applyLocalConfigOverlay(plan.Config)
		if err != nil {
			return CredentialPlan{}, err
		}
		return plan, nil
	}

	credential, err := topology.CredentialConfig(credentialName)
	if err != nil {
		return CredentialPlan{}, err
	}
	plan.Credential = credential
	plan.CredentialName = credential.Name
	plan.Config = cfg.WithCredential(credential.Name, credential)
	plan.Config, _, err = applyLocalConfigOverlay(plan.Config)
	if err != nil {
		return CredentialPlan{}, err
	}
	plan.Bindings, plan.AuthDrivers, err = resolveCredentialBindings(topology, credential.Name)
	if err != nil {
		return CredentialPlan{}, err
	}
	return plan, nil
}

func resolveBootstrapCredentialPlan(cfg appconfig.Config) (CredentialPlan, error) {
	plan := CredentialPlan{
		Config: cfg,
	}
	resolvedCfg, localCfg, err := applyLocalConfigOverlay(plan.Config)
	if err != nil {
		return CredentialPlan{}, err
	}
	plan.Config = resolvedCfg
	if len(plan.AuthDrivers) == 0 {
		plan.AuthDrivers = append([]string(nil), localCfg.Drivers...)
	}
	return plan, nil
}

func resolveBootstrapCredentialPlanForCommand(cfg appconfig.Config, explicit string) (CredentialPlan, error) {
	plan, err := resolveBootstrapCredentialPlan(cfg)
	if err != nil {
		return CredentialPlan{}, err
	}

	explicit = strings.TrimSpace(explicit)
	if explicit == "" {
		return plan, nil
	}

	credential := appconfig.Credential{
		Name: explicit,
		Kind: appconfig.CredentialKindOAuth,
	}
	plan.Credential = credential
	plan.CredentialName = explicit
	plan.Config = plan.Config.WithCredential(explicit, credential)
	var localCfg appconfig.LocalConfig
	plan.Config, localCfg, err = applyLocalConfigOverlay(plan.Config)
	if err != nil {
		return CredentialPlan{}, err
	}
	if len(plan.AuthDrivers) == 0 {
		plan.AuthDrivers = append([]string(nil), localCfg.Drivers...)
	}
	return plan, nil
}

func isDefaultCredentialTopologyPath(cfg appconfig.Config, topologyPath string) bool {
	defaultPath := appconfig.DefaultTopologyPath(cfg.Auth.StateDir)
	if strings.TrimSpace(defaultPath) == "" {
		return false
	}
	return filepath.Clean(strings.TrimSpace(topologyPath)) == filepath.Clean(defaultPath)
}

func (p CredentialPlan) BindingNames(kind CredentialBindingKind) []string {
	names := make([]string, 0, len(p.Bindings))
	for _, binding := range p.Bindings {
		if binding.Kind != kind {
			continue
		}
		names = append(names, strings.TrimSpace(binding.Name))
	}
	return names
}

func resolveCredentialBindings(topology appconfig.Topology, credentialName string) ([]CredentialBinding, []string, error) {
	credentialName = strings.TrimSpace(credentialName)
	if credentialName == "" {
		return nil, nil, nil
	}

	bindings := make([]CredentialBinding, 0)
	driversSeen := make(map[string]struct{})

	for _, source := range topology.Sources {
		sourceSpec, ok := providers.LookupSourceSpec(source.Driver)
		if !ok {
			return nil, nil, fmt.Errorf("source %s 不支持 driver: %s", source.Name, source.Driver)
		}
		if !sourceSpec.RequiresCredential {
			continue
		}

		matched, implicit, err := credentialBindingMatch(topology, source.CredentialRef, credentialName)
		if err != nil {
			return nil, nil, err
		}
		if !matched {
			continue
		}

		driver := strings.TrimSpace(source.Driver)
		bindings = append(bindings, CredentialBinding{
			Kind:     CredentialBindingSource,
			Name:     strings.TrimSpace(source.Name),
			Driver:   driver,
			Implicit: implicit,
		})
		driversSeen[driver] = struct{}{}
	}

	for _, sink := range topology.Sinks {
		sinkSpec, ok := providers.LookupSinkSpec(sink.Driver)
		if !ok {
			return nil, nil, fmt.Errorf("sink %s 不支持 driver: %s", sink.Name, sink.Driver)
		}
		if !sinkSpec.RequiresCredential {
			continue
		}

		matched, implicit, err := credentialBindingMatch(topology, sink.CredentialRef, credentialName)
		if err != nil {
			return nil, nil, err
		}
		if !matched {
			continue
		}

		driver := strings.TrimSpace(sink.Driver)
		bindings = append(bindings, CredentialBinding{
			Kind:     CredentialBindingSink,
			Name:     strings.TrimSpace(sink.Name),
			Driver:   driver,
			Implicit: implicit,
		})
		driversSeen[driver] = struct{}{}
	}

	sort.Slice(bindings, func(i, j int) bool {
		left := bindings[i]
		right := bindings[j]
		if rank := credentialBindingKindRank(left.Kind) - credentialBindingKindRank(right.Kind); rank != 0 {
			return rank < 0
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Driver < right.Driver
	})

	drivers := make([]string, 0, len(driversSeen))
	for driver := range driversSeen {
		drivers = append(drivers, driver)
	}
	sort.Strings(drivers)

	return bindings, drivers, nil
}

func resolveCredentialBindingsForCommand(topology appconfig.Topology, credentialName string) ([]CredentialBinding, []string) {
	bindings, drivers, err := resolveCredentialBindings(topology, credentialName)
	if err == nil {
		return bindings, drivers
	}
	return nil, nil
}

func resolveCredentialRefForCommand(topology appconfig.Topology, explicit string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		if _, err := topology.CredentialConfig(explicit); err != nil {
			return "", err
		}
		return explicit, nil
	}

	resolved, err := topology.ResolveCredentialRef("")
	if err != nil {
		return "", nil
	}
	return resolved, nil
}

func credentialBindingKindRank(kind CredentialBindingKind) int {
	switch kind {
	case CredentialBindingSource:
		return 0
	case CredentialBindingSink:
		return 1
	default:
		return 2
	}
}

func credentialBindingMatch(topology appconfig.Topology, credentialRef, credentialName string) (bool, bool, error) {
	credentialRef = strings.TrimSpace(credentialRef)
	credentialName = strings.TrimSpace(credentialName)
	if credentialName == "" {
		return false, false, nil
	}
	if credentialRef != "" {
		if _, err := topology.CredentialConfig(credentialRef); err != nil {
			return false, false, err
		}
		return credentialRef == credentialName, false, nil
	}

	implicitRef := implicitCredentialRef(topology)
	if implicitRef == "" {
		return false, false, nil
	}
	return implicitRef == credentialName, true, nil
}

func implicitCredentialRef(topology appconfig.Topology) string {
	if value := strings.TrimSpace(topology.DefaultCredential); value != "" {
		return value
	}
	if _, ok := topology.Credentials[implicitDefaultCredentialName]; ok {
		return implicitDefaultCredentialName
	}
	if len(topology.Credentials) == 1 {
		for name := range topology.Credentials {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func applyLocalConfigOverlay(cfg appconfig.Config) (appconfig.Config, appconfig.LocalConfig, error) {
	stateDir := strings.TrimSpace(cfg.Auth.StateDir)
	if stateDir == "" {
		return cfg, appconfig.LocalConfig{}, nil
	}
	localCfg, err := appconfig.LoadLocalConfig(stateDir)
	if err != nil {
		return appconfig.Config{}, appconfig.LocalConfig{}, err
	}
	return cfg.WithLocalConfig(localCfg), localCfg, nil
}
