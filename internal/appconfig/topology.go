package appconfig

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultTopologyCredentialName = "default"
	defaultTopologyRouteName      = "default"
	defaultTopologySourceName     = "default"
)

type TopologyOptions struct {
	IncludeExisting bool
	WriteBack       bool
	VerifyWriteBack bool
	DeleteSource    bool
}

// Topology 表示命名 source / sink / route / credential 的配置拓扑。
// 当前仍由单 provider 环境变量编译出一个默认 topology，后续再扩展到多来源多出口。
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
	Name             string            `json:"name,omitempty"`
	Kind             string            `json:"kind,omitempty"`
	StateDir         string            `json:"state_dir,omitempty"`
	ClientID         string            `json:"client_id,omitempty"`
	Tenant           string            `json:"tenant,omitempty"`
	AuthorityBaseURL string            `json:"authority_base_url,omitempty"`
	TokenStore       string            `json:"token_store,omitempty"`
	KeyringService   string            `json:"keyring_service,omitempty"`
	GraphScopes      []string          `json:"graph_scopes,omitempty"`
	EWSScopes        []string          `json:"ews_scopes,omitempty"`
	IMAPScopes       []string          `json:"imap_scopes,omitempty"`
	IMAPUsername     string            `json:"imap_username,omitempty"`
	Options          map[string]string `json:"options,omitempty"`
}

type Source struct {
	Name            string            `json:"name,omitempty"`
	Driver          string            `json:"driver,omitempty"`
	Mode            string            `json:"mode,omitempty"`
	CredentialRef   string            `json:"credential_ref,omitempty"`
	Folder          string            `json:"folder,omitempty"`
	StatePath       string            `json:"state_path,omitempty"`
	IncludeExisting bool              `json:"include_existing,omitempty"`
	PollInterval    time.Duration     `json:"poll_interval,omitempty"`
	CycleTimeout    time.Duration     `json:"cycle_timeout,omitempty"`
	Options         map[string]string `json:"options,omitempty"`
}

type Sink struct {
	Name          string            `json:"name,omitempty"`
	Driver        string            `json:"driver,omitempty"`
	CredentialRef string            `json:"credential_ref,omitempty"`
	OutputDir     string            `json:"output_dir,omitempty"`
	Folder        string            `json:"folder,omitempty"`
	Verify        bool              `json:"verify,omitempty"`
	Options       map[string]string `json:"options,omitempty"`
}

type Route struct {
	Name         string             `json:"name,omitempty"`
	SourceRefs   []string           `json:"source_refs,omitempty"`
	StateDir     string             `json:"state_dir,omitempty"`
	Targets      []RouteTarget      `json:"targets,omitempty"`
	DeleteSource DeleteSourcePolicy `json:"delete_source,omitempty"`
	Options      map[string]string  `json:"options,omitempty"`
}

type RouteTarget struct {
	Name     string            `json:"name,omitempty"`
	SinkRef  string            `json:"sink_ref,omitempty"`
	Artifact string            `json:"artifact,omitempty"`
	Required bool              `json:"required,omitempty"`
	Options  map[string]string `json:"options,omitempty"`
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

func (c Config) BuildTopology(options TopologyOptions) (Topology, error) {
	if options.VerifyWriteBack && !options.WriteBack {
		return Topology{}, fmt.Errorf("verify write back 依赖 write back")
	}
	if options.DeleteSource && !options.WriteBack {
		return Topology{}, fmt.Errorf("delete source 依赖 write back")
	}

	topology := Topology{
		Credentials: map[string]Credential{
			defaultTopologyCredentialName: {
				Name: defaultTopologyCredentialName,
				Kind: "shared-session",
			},
		},
		Sources: map[string]Source{
			defaultTopologySourceName: {
				Name:            defaultTopologySourceName,
				Driver:          normalizeTopologyDriver(c.Provider, defaultProvider),
				Mode:            "poll",
				CredentialRef:   defaultTopologyCredentialName,
				Folder:          c.Mail.Sync.Folder,
				IncludeExisting: options.IncludeExisting,
				PollInterval:    c.Mail.Sync.PollInterval,
				CycleTimeout:    c.Mail.Sync.CycleTimeout,
			},
		},
		Sinks: map[string]Sink{},
		Routes: map[string]Route{
			defaultTopologyRouteName: {
				Name:       defaultTopologyRouteName,
				SourceRefs: []string{defaultTopologySourceName},
			},
		},
		DefaultSource:     defaultTopologySourceName,
		DefaultRoute:      defaultTopologyRouteName,
		DefaultCredential: defaultTopologyCredentialName,
	}
	source := topology.Sources[defaultTopologySourceName]
	source.StatePath = c.Mail.FlowProducerStatePathFor(source.Name, source.Driver, source.Folder)
	topology.Sources[defaultTopologySourceName] = source
	route := topology.Routes[defaultTopologyRouteName]
	route.StateDir = c.Mail.FlowStateDirFor(route.Name, source.Name, source.Driver, source.Folder)
	topology.Routes[defaultTopologyRouteName] = route

	if !c.Mail.Pipeline.SaveOutput && !options.WriteBack {
		topology.Sinks["discard"] = Sink{
			Name:   "discard",
			Driver: "discard",
		}
		route.Targets = append(route.Targets, RouteTarget{
			Name:     "discard-primary",
			SinkRef:  "discard",
			Artifact: "primary",
			Required: true,
		})
	}
	if c.Mail.Pipeline.SaveOutput {
		topology.Sinks["local-output"] = Sink{
			Name:      "local-output",
			Driver:    "file",
			OutputDir: c.Mail.Pipeline.OutputDir,
		}
		route.Targets = append(route.Targets, RouteTarget{
			Name:     "local-output",
			SinkRef:  "local-output",
			Artifact: "primary",
			Required: true,
		})
	}
	if options.WriteBack {
		topology.Sinks["write-back"] = Sink{
			Name:          "write-back",
			Driver:        normalizeTopologyDriver(c.Mail.Pipeline.WriteBackProvider, defaultWriteBackProvider),
			CredentialRef: defaultTopologyCredentialName,
			Folder:        strings.TrimSpace(c.Mail.Pipeline.WriteBackFolder),
			Verify:        options.VerifyWriteBack,
		}
		route.Targets = append(route.Targets, RouteTarget{
			Name:     "write-back",
			SinkRef:  "write-back",
			Artifact: "primary",
			Required: true,
		})
	}
	if options.DeleteSource {
		route.DeleteSource = DeleteSourcePolicy{
			Enabled:          true,
			RequireSameStore: true,
			EligibleSinks:    []string{"write-back"},
		}
	}
	topology.Routes[defaultTopologyRouteName] = route

	if err := topology.Validate(); err != nil {
		return Topology{}, err
	}
	return topology, nil
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
	if strings.TrimSpace(s.Mode) == "" {
		return fmt.Errorf("source %s 缺少 mode", name)
	}
	if ref := strings.TrimSpace(s.CredentialRef); ref != "" {
		if _, ok := credentials[ref]; !ok {
			return fmt.Errorf("source %s 引用了不存在的 credential: %s", name, ref)
		}
	}
	if strings.EqualFold(strings.TrimSpace(s.Mode), "poll") {
		if strings.TrimSpace(s.StatePath) == "" {
			return fmt.Errorf("source %s 缺少 state path", name)
		}
		if s.PollInterval <= 0 {
			return fmt.Errorf("source %s poll interval 必须大于 0", name)
		}
		if s.CycleTimeout <= 0 {
			return fmt.Errorf("source %s cycle timeout 必须大于 0", name)
		}
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
	if ref := strings.TrimSpace(s.CredentialRef); ref != "" {
		if _, ok := credentials[ref]; !ok {
			return fmt.Errorf("sink %s 引用了不存在的 credential: %s", name, ref)
		}
	}
	if strings.EqualFold(strings.TrimSpace(s.Driver), "file") && strings.TrimSpace(s.OutputDir) == "" {
		return fmt.Errorf("sink %s 缺少 output dir", name)
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
