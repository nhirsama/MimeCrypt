package appconfig

import (
	"fmt"
	"strings"
)

// InstanceKind 标识 topology 中的命名配置实例类别。
type InstanceKind string

const (
	InstanceKindCredential InstanceKind = "credential"
	InstanceKindSource     InstanceKind = "source"
	InstanceKindSink       InstanceKind = "sink"
	InstanceKindRoute      InstanceKind = "route"
)

// ConfiguredInstanceSummary 是声明式配置实例的稳定摘要。
// 它只描述 topology 中的命名实例，不承载运行时资源。
type ConfiguredInstanceSummary struct {
	Kind          InstanceKind
	Name          string
	Driver        string
	CredentialRef string
}

func (c Credential) Summary() ConfiguredInstanceSummary {
	return ConfiguredInstanceSummary{
		Kind:   InstanceKindCredential,
		Name:   strings.TrimSpace(c.Name),
		Driver: strings.TrimSpace(c.Kind),
	}
}

func (s Source) Summary() ConfiguredInstanceSummary {
	return ConfiguredInstanceSummary{
		Kind:          InstanceKindSource,
		Name:          strings.TrimSpace(s.Name),
		Driver:        strings.TrimSpace(s.Driver),
		CredentialRef: strings.TrimSpace(s.CredentialRef),
	}
}

func (s Sink) Summary() ConfiguredInstanceSummary {
	return ConfiguredInstanceSummary{
		Kind:          InstanceKindSink,
		Name:          strings.TrimSpace(s.Name),
		Driver:        strings.TrimSpace(s.Driver),
		CredentialRef: strings.TrimSpace(s.CredentialRef),
	}
}

func (r Route) Summary() ConfiguredInstanceSummary {
	return ConfiguredInstanceSummary{
		Kind: InstanceKindRoute,
		Name: strings.TrimSpace(r.Name),
	}
}

func (s Source) Configured() Source {
	s.StatePath = ""
	s.DriverConfig = cloneRawMessage(s.DriverConfig)
	return s
}

func (s Source) WithRuntimeStatePath(statePath string) Source {
	s = s.Configured()
	s.StatePath = strings.TrimSpace(statePath)
	return s
}

func (r Route) Configured() Route {
	r.StateDir = ""
	return r
}

func (r Route) WithRuntimeStateDir(stateDir string) Route {
	r = r.Configured()
	r.StateDir = strings.TrimSpace(stateDir)
	return r
}

func (t Topology) SourceInstance(name string) (Source, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Source{}, fmt.Errorf("source 未配置")
	}
	source, ok := t.Sources[name]
	if !ok {
		return Source{}, fmt.Errorf("source 不存在: %s", name)
	}
	return source.Configured(), nil
}

func (t Topology) SinkInstance(name string) (Sink, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Sink{}, fmt.Errorf("sink 未配置")
	}
	sink, ok := t.Sinks[name]
	if !ok {
		return Sink{}, fmt.Errorf("sink 不存在: %s", name)
	}
	return sink, nil
}

func (t Topology) RouteInstance(name string) (Route, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Route{}, fmt.Errorf("route 未配置")
	}
	route, ok := t.Routes[name]
	if !ok {
		return Route{}, fmt.Errorf("route 不存在: %s", name)
	}
	return route.Configured(), nil
}
