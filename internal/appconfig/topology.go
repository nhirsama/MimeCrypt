package appconfig

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mimecrypt/internal/provider"
)

const (
	defaultTopologyCredentialName = "default"
)

// Topology 表示命名 source / sink / route / credential 的配置拓扑。
// 当前运行时完全基于该显式模型进行解析与装配。
type Topology struct {
	Credentials       map[string]Credential `json:"credentials,omitempty"`
	Sources           map[string]Source     `json:"sources,omitempty"`
	Sinks             map[string]Sink       `json:"sinks,omitempty"`
	Routes            map[string]Route      `json:"routes,omitempty"`
	DefaultCredential string                `json:"default_credential,omitempty"`
	DefaultSource     string                `json:"default_source,omitempty"`
	DefaultRoute      string                `json:"default_route,omitempty"`
}

type Credential struct {
	Name             string   `json:"name,omitempty"`
	Kind             string   `json:"kind,omitempty"`
	StateDir         string   `json:"state_dir,omitempty"`
	ClientID         string   `json:"client_id,omitempty"`
	Tenant           string   `json:"tenant,omitempty"`
	AuthorityBaseURL string   `json:"authority_base_url,omitempty"`
	TokenStore       string   `json:"token_store,omitempty"`
	KeyringService   string   `json:"keyring_service,omitempty"`
	GraphScopes      []string `json:"graph_scopes,omitempty"`
	EWSScopes        []string `json:"ews_scopes,omitempty"`
	IMAPScopes       []string `json:"imap_scopes,omitempty"`
	IMAPUsername     string   `json:"imap_username,omitempty"`
	GraphScopesSet   bool     `json:"-"`
	EWSScopesSet     bool     `json:"-"`
	IMAPScopesSet    bool     `json:"-"`
}

type Source struct {
	Name            string        `json:"name,omitempty"`
	Driver          string        `json:"driver,omitempty"`
	Mode            string        `json:"mode,omitempty"`
	CredentialRef   string        `json:"credential_ref,omitempty"`
	Folder          string        `json:"folder,omitempty"`
	StatePath       string        `json:"state_path,omitempty"`
	IncludeExisting bool          `json:"include_existing,omitempty"`
	PollInterval    time.Duration `json:"poll_interval,omitempty"`
	CycleTimeout    time.Duration `json:"cycle_timeout,omitempty"`
}

type Sink struct {
	Name          string `json:"name,omitempty"`
	Driver        string `json:"driver,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
	OutputDir     string `json:"output_dir,omitempty"`
	Folder        string `json:"folder,omitempty"`
	Verify        bool   `json:"verify,omitempty"`
}

type Route struct {
	Name         string             `json:"name,omitempty"`
	SourceRefs   []string           `json:"source_refs,omitempty"`
	StateDir     string             `json:"state_dir,omitempty"`
	Targets      []RouteTarget      `json:"targets,omitempty"`
	DeleteSource DeleteSourcePolicy `json:"delete_source,omitempty"`
}

type RouteTarget struct {
	Name     string `json:"name,omitempty"`
	SinkRef  string `json:"sink_ref,omitempty"`
	Artifact string `json:"artifact,omitempty"`
	Required bool   `json:"required,omitempty"`
}

type DeleteSourcePolicy struct {
	Enabled          bool     `json:"enabled,omitempty"`
	RequireSameStore bool     `json:"require_same_store,omitempty"`
	EligibleSinks    []string `json:"eligible_sinks,omitempty"`
}

func (t Topology) Normalize() Topology {
	if t.Credentials != nil {
		for name, credential := range t.Credentials {
			if strings.TrimSpace(credential.Name) == "" {
				credential.Name = strings.TrimSpace(name)
				t.Credentials[name] = credential
			}
		}
	}
	if t.Sources != nil {
		for name, source := range t.Sources {
			if strings.TrimSpace(source.Name) == "" {
				source.Name = strings.TrimSpace(name)
				t.Sources[name] = source
			}
		}
	}
	if t.Sinks != nil {
		for name, sink := range t.Sinks {
			if strings.TrimSpace(sink.Name) == "" {
				sink.Name = strings.TrimSpace(name)
				t.Sinks[name] = sink
			}
		}
	}
	if t.Routes != nil {
		for name, route := range t.Routes {
			if strings.TrimSpace(route.Name) == "" {
				route.Name = strings.TrimSpace(name)
				t.Routes[name] = route
			}
		}
	}
	return t
}

func (c *Credential) UnmarshalJSON(data []byte) error {
	type credentialAlias Credential

	var decoded credentialAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*c = Credential(decoded)
	_, c.GraphScopesSet = raw["graph_scopes"]
	_, c.EWSScopesSet = raw["ews_scopes"]
	_, c.IMAPScopesSet = raw["imap_scopes"]
	return nil
}

func (t Topology) ValidateStructure() error {
	if len(t.Sources) == 0 {
		return fmt.Errorf("至少需要一个 source")
	}
	if len(t.Routes) == 0 {
		return fmt.Errorf("至少需要一个 route")
	}
	if strings.TrimSpace(t.DefaultCredential) != "" {
		if _, err := t.DefaultCredentialConfig(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(t.DefaultSource) != "" {
		if _, err := t.DefaultSourceConfig(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(t.DefaultRoute) != "" {
		if _, err := t.DefaultRouteConfig(); err != nil {
			return err
		}
	}
	for name, credential := range t.Credentials {
		if err := credential.Validate(name); err != nil {
			return err
		}
	}
	for name, source := range t.Sources {
		if err := source.Validate(name, t.Credentials); err != nil {
			return err
		}
	}
	for name, sink := range t.Sinks {
		if err := sink.Validate(name, t.Credentials); err != nil {
			return err
		}
	}
	for name, route := range t.Routes {
		if err := route.Validate(name, t.Sources, t.Sinks); err != nil {
			return err
		}
	}
	return nil
}

func (t Topology) Validate() error {
	if err := t.ValidateStructure(); err != nil {
		return err
	}
	if strings.TrimSpace(t.DefaultSource) == "" {
		return fmt.Errorf("default source 未配置")
	}
	if strings.TrimSpace(t.DefaultRoute) == "" {
		return fmt.Errorf("default route 未配置")
	}
	return nil
}

func (t Topology) CredentialConfig(name string) (Credential, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Credential{}, fmt.Errorf("credential 未配置")
	}
	credential, ok := t.Credentials[name]
	if !ok {
		return Credential{}, fmt.Errorf("credential 不存在: %s", name)
	}
	return credential, nil
}

func (t Topology) ResolveCredentialRef(explicit string) (string, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		if _, err := t.CredentialConfig(value); err != nil {
			return "", err
		}
		return value, nil
	}
	if value := strings.TrimSpace(t.DefaultCredential); value != "" {
		if _, err := t.CredentialConfig(value); err != nil {
			return "", err
		}
		return value, nil
	}
	if len(t.Credentials) == 0 {
		return "", nil
	}
	if _, ok := t.Credentials[defaultTopologyCredentialName]; ok {
		return defaultTopologyCredentialName, nil
	}
	if len(t.Credentials) == 1 {
		for name := range t.Credentials {
			return strings.TrimSpace(name), nil
		}
	}
	return "", fmt.Errorf("topology 存在多个 credential，请显式设置 credential_ref 或 default_credential")
}

func (t Topology) DefaultCredentialConfig() (Credential, error) {
	name := strings.TrimSpace(t.DefaultCredential)
	if name == "" {
		return Credential{}, fmt.Errorf("default credential 未配置")
	}
	return t.CredentialConfig(name)
}

func (t Topology) DefaultSourceConfig() (Source, error) {
	name := strings.TrimSpace(t.DefaultSource)
	if name == "" {
		return Source{}, fmt.Errorf("default source 未配置")
	}
	source, ok := t.Sources[name]
	if !ok {
		return Source{}, fmt.Errorf("default source 不存在: %s", name)
	}
	return source, nil
}

func (t Topology) DefaultRouteConfig() (Route, error) {
	name := strings.TrimSpace(t.DefaultRoute)
	if name == "" {
		return Route{}, fmt.Errorf("default route 未配置")
	}
	route, ok := t.Routes[name]
	if !ok {
		return Route{}, fmt.Errorf("default route 不存在: %s", name)
	}
	return route, nil
}

func (c Credential) Validate(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("credential name 不能为空")
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("credential %s 缺少 name", name)
	}
	if strings.TrimSpace(c.Name) != strings.TrimSpace(name) {
		return fmt.Errorf("credential %s 的 name 必须与配置键一致", name)
	}
	if strings.TrimSpace(c.Kind) == "" {
		return fmt.Errorf("credential %s 缺少 kind", name)
	}
	return nil
}

func (s Source) Validate(name string, credentials map[string]Credential) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("source name 不能为空")
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("source %s 缺少 name", name)
	}
	if strings.TrimSpace(s.Name) != strings.TrimSpace(name) {
		return fmt.Errorf("source %s 的 name 必须与配置键一致", name)
	}
	if strings.TrimSpace(s.Driver) == "" {
		return fmt.Errorf("source %s 缺少 driver", name)
	}
	sourceSpec, ok := provider.LookupSourceSpec(s.Driver)
	if !ok {
		return fmt.Errorf("source %s 不支持 driver: %s", name, s.Driver)
	}
	if strings.TrimSpace(s.Mode) == "" {
		return fmt.Errorf("source %s 缺少 mode", name)
	}
	modeSpec, ok := sourceSpec.ModeSpec(s.Mode)
	if !ok {
		return fmt.Errorf("source %s 的 driver %s 不支持 mode: %s", name, s.Driver, s.Mode)
	}
	if ref := strings.TrimSpace(s.CredentialRef); ref != "" {
		if !sourceSpec.RequiresCredential {
			return fmt.Errorf("source %s 的 driver %s 不接受 credential_ref", name, s.Driver)
		}
		if _, ok := credentials[ref]; !ok {
			return fmt.Errorf("source %s 引用了不存在的 credential: %s", name, ref)
		}
	}
	if modeSpec.RequiresStatePath && strings.TrimSpace(s.StatePath) == "" {
		return fmt.Errorf("source %s 缺少 state path", name)
	}
	if modeSpec.RequiresPollInterval && s.PollInterval <= 0 {
		return fmt.Errorf("source %s poll interval 必须大于 0", name)
	}
	if modeSpec.RequiresCycleTimeout && s.CycleTimeout <= 0 {
		return fmt.Errorf("source %s cycle timeout 必须大于 0", name)
	}
	return nil
}

func (s Sink) Validate(name string, credentials map[string]Credential) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("sink name 不能为空")
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("sink %s 缺少 name", name)
	}
	if strings.TrimSpace(s.Name) != strings.TrimSpace(name) {
		return fmt.Errorf("sink %s 的 name 必须与配置键一致", name)
	}
	if strings.TrimSpace(s.Driver) == "" {
		return fmt.Errorf("sink %s 缺少 driver", name)
	}
	sinkSpec, ok := provider.LookupSinkSpec(s.Driver)
	if !ok {
		return fmt.Errorf("sink %s 不支持 driver: %s", name, s.Driver)
	}
	if ref := strings.TrimSpace(s.CredentialRef); ref != "" {
		if !sinkSpec.RequiresCredential {
			return fmt.Errorf("sink %s 的 driver %s 不接受 credential_ref", name, s.Driver)
		}
		if _, ok := credentials[ref]; !ok {
			return fmt.Errorf("sink %s 引用了不存在的 credential: %s", name, ref)
		}
	}
	if sinkSpec.RequiresOutputDir && strings.TrimSpace(s.OutputDir) == "" {
		return fmt.Errorf("sink %s 缺少 output dir", name)
	}
	if s.Verify && !sinkSpec.SupportsVerify {
		return fmt.Errorf("sink %s 的 driver %s 不支持 verify", name, s.Driver)
	}
	return nil
}

func (r Route) Validate(name string, sources map[string]Source, sinks map[string]Sink) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("route name 不能为空")
	}
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("route %s 缺少 name", name)
	}
	if strings.TrimSpace(r.Name) != strings.TrimSpace(name) {
		return fmt.Errorf("route %s 的 name 必须与配置键一致", name)
	}
	if len(r.SourceRefs) == 0 {
		return fmt.Errorf("route %s 至少需要一个 source", name)
	}
	for _, ref := range r.SourceRefs {
		if _, ok := sources[strings.TrimSpace(ref)]; !ok {
			return fmt.Errorf("route %s 引用了不存在的 source: %s", name, ref)
		}
	}
	if len(r.Targets) == 0 {
		return fmt.Errorf("route %s 至少需要一个 sink target", name)
	}
	seen := make(map[string]struct{}, len(r.Targets))
	for _, target := range r.Targets {
		if err := target.Validate(sinks); err != nil {
			return fmt.Errorf("route %s target 校验失败: %w", name, err)
		}
		key := target.Key()
		if _, exists := seen[key]; exists {
			return fmt.Errorf("route %s 存在重复 target: %s", name, key)
		}
		seen[key] = struct{}{}
	}
	if r.DeleteSource.Enabled && len(r.DeleteSource.EligibleSinks) == 0 {
		return fmt.Errorf("route %s 启用 delete source 时必须声明 eligible sinks", name)
	}
	if r.DeleteSource.Enabled {
		for _, sourceRef := range r.SourceRefs {
			source, ok := sources[strings.TrimSpace(sourceRef)]
			if !ok {
				continue
			}
			sourceSpec, ok := provider.LookupSourceSpec(source.Driver)
			if !ok || !sourceSpec.SupportsDelete {
				return fmt.Errorf("route %s 启用 delete source 时，source %s 的 driver %s 不支持删除", name, source.Name, source.Driver)
			}
			switch sourceSpec.DeleteSemantics {
			case provider.DeleteSemanticsHard:
			case provider.DeleteSemanticsSoft:
				return fmt.Errorf("route %s 启用 delete source 时，source %s 的 driver %s 仅支持 soft delete", name, source.Name, source.Driver)
			default:
				return fmt.Errorf("route %s 启用 delete source 时，source %s 的 driver %s 删除语义未知", name, source.Name, source.Driver)
			}
		}
	}
	for _, ref := range r.DeleteSource.EligibleSinks {
		if _, ok := sinks[strings.TrimSpace(ref)]; !ok {
			return fmt.Errorf("route %s delete source 引用了不存在的 sink: %s", name, ref)
		}
	}
	return nil
}

func (t RouteTarget) Validate(sinks map[string]Sink) error {
	if strings.TrimSpace(t.SinkRef) == "" {
		return fmt.Errorf("sink ref 不能为空")
	}
	if _, ok := sinks[strings.TrimSpace(t.SinkRef)]; !ok {
		return fmt.Errorf("不存在的 sink: %s", t.SinkRef)
	}
	return nil
}

func (t RouteTarget) Key() string {
	sinkRef := strings.TrimSpace(t.SinkRef)
	artifact := strings.TrimSpace(t.Artifact)
	if artifact == "" {
		artifact = "primary"
	}
	return sinkRef + ":" + artifact
}

func normalizeTopologyDriver(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return strings.ToLower(strings.TrimSpace(fallback))
	}
	return value
}
