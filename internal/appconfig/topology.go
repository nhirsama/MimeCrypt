package appconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	defaultTopologyCredentialName = "default"
)

// Topology 表示命名 configured instance 组成的声明式拓扑。
// 这些实例只描述 source / sink / credential / route 的配置，不持有运行时资源。
type Topology struct {
	Credentials       map[string]Credential `json:"credentials,omitempty"`
	Sources           map[string]Source     `json:"sources,omitempty"`
	Sinks             map[string]Sink       `json:"sinks,omitempty"`
	Routes            map[string]Route      `json:"routes,omitempty"`
	DefaultCredential string                `json:"default_credential,omitempty"`
	DefaultSource     string                `json:"default_source,omitempty"`
	DefaultRoute      string                `json:"default_route,omitempty"`
}

// Credential 是 topology 中的命名配置实例。
// 它只描述声明式 credential 配置，不承载运行时 session 或 token handle。
// Credential 表示命名 credential 配置实例。
// 它负责声明认证材料覆盖和会话持久化策略。
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

// Source 是 topology 中的命名 source device 配置实例。
// 它声明 source driver、接入模式和驱动配置；运行时对象由 compiled plan 打开。
// Source 表示命名 source device 配置实例。
// 它描述 source driver、接入 mode、credential 绑定和驱动私有配置。
type Source struct {
	Name          string `json:"name,omitempty"`
	Driver        string `json:"driver,omitempty"`
	Mode          string `json:"mode,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
	Folder        string `json:"folder,omitempty"`
	// StatePath 是 compiled runtime 注入的派生值，不属于 topology 配置持久化。
	StatePath       string         `json:"-"`
	IncludeExisting bool           `json:"include_existing,omitempty"`
	PollInterval    time.Duration  `json:"poll_interval,omitempty"`
	CycleTimeout    time.Duration  `json:"cycle_timeout,omitempty"`
	Webhook         *WebhookSource `json:"webhook,omitempty"`
}

type WebhookSource struct {
	ListenAddr         string        `json:"listen_addr,omitempty"`
	Path               string        `json:"path,omitempty"`
	SecretEnv          string        `json:"secret_env,omitempty"`
	MaxBodyBytes       int64         `json:"max_body_bytes,omitempty"`
	TimestampTolerance time.Duration `json:"timestamp_tolerance,omitempty"`
}

// Sink 是 topology 中的命名 sink device 配置实例。
// 它声明 sink driver 和目标配置；运行时 consumer 由 compiled plan 打开。
// Sink 表示命名 sink device 配置实例。
// 它描述 sink driver、credential 绑定和输出位置。
type Sink struct {
	Name          string `json:"name,omitempty"`
	Driver        string `json:"driver,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
	OutputDir     string `json:"output_dir,omitempty"`
	Folder        string `json:"folder,omitempty"`
	Verify        bool   `json:"verify,omitempty"`
}

// Route 是 topology 中的命名编排实例，用于把 source 与 sink 连接为邮件事务链路。
// Route 表示命名 route 配置实例。
// 它只描述 source 与 sink 的声明式关系，运行时派生值在 compiled plan 中展开。
type Route struct {
	Name       string   `json:"name,omitempty"`
	SourceRefs []string `json:"source_refs,omitempty"`
	// StateDir 是 compiled runtime 注入的派生值，不属于 topology 配置持久化。
	StateDir     string             `json:"-"`
	Targets      []RouteTarget      `json:"targets,omitempty"`
	DeleteSource DeleteSourcePolicy `json:"delete_source,omitempty"`
}

type RouteTarget struct {
	Name    string `json:"name,omitempty"`
	SinkRef string `json:"sink_ref,omitempty"`
	// Artifact 作为路由角色标签保留，当前所有 sink 都消费统一邮件对象。
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
			source.StatePath = ""
			if strings.TrimSpace(source.Name) == "" {
				source.Name = strings.TrimSpace(name)
			}
			t.Sources[name] = source
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
			route.StateDir = ""
			if strings.TrimSpace(route.Name) == "" {
				route.Name = strings.TrimSpace(name)
			}
			t.Routes[name] = route
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

func (s *Source) UnmarshalJSON(data []byte) error {
	type sourceJSON struct {
		Name            string         `json:"name,omitempty"`
		Driver          string         `json:"driver,omitempty"`
		Mode            string         `json:"mode,omitempty"`
		CredentialRef   string         `json:"credential_ref,omitempty"`
		Folder          string         `json:"folder,omitempty"`
		StatePath       string         `json:"state_path,omitempty"`
		IncludeExisting bool           `json:"include_existing,omitempty"`
		PollInterval    time.Duration  `json:"poll_interval,omitempty"`
		CycleTimeout    time.Duration  `json:"cycle_timeout,omitempty"`
		Webhook         *WebhookSource `json:"webhook,omitempty"`
	}

	var decoded sourceJSON
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}

	*s = Source{
		Name:            decoded.Name,
		Driver:          decoded.Driver,
		Mode:            decoded.Mode,
		CredentialRef:   decoded.CredentialRef,
		Folder:          decoded.Folder,
		IncludeExisting: decoded.IncludeExisting,
		PollInterval:    decoded.PollInterval,
		CycleTimeout:    decoded.CycleTimeout,
		Webhook:         decoded.Webhook,
	}
	return nil
}

func (r *Route) UnmarshalJSON(data []byte) error {
	type routeJSON struct {
		Name         string             `json:"name,omitempty"`
		SourceRefs   []string           `json:"source_refs,omitempty"`
		StateDir     string             `json:"state_dir,omitempty"`
		Targets      []RouteTarget      `json:"targets,omitempty"`
		DeleteSource DeleteSourcePolicy `json:"delete_source,omitempty"`
	}

	var decoded routeJSON
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}

	*r = Route{
		Name:         decoded.Name,
		SourceRefs:   append([]string(nil), decoded.SourceRefs...),
		Targets:      append([]RouteTarget(nil), decoded.Targets...),
		DeleteSource: decoded.DeleteSource,
	}
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
	source, err := t.SourceInstance(name)
	if err != nil {
		return Source{}, fmt.Errorf("default source 不存在: %s", name)
	}
	return source, nil
}

func (t Topology) DefaultRouteConfig() (Route, error) {
	name := strings.TrimSpace(t.DefaultRoute)
	if name == "" {
		return Route{}, fmt.Errorf("default route 未配置")
	}
	route, err := t.RouteInstance(name)
	if err != nil {
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
	if _, ok := LookupCredentialKindSpec(c.Kind); !ok {
		return fmt.Errorf("credential %s 不支持 kind: %s", name, strings.TrimSpace(c.Kind))
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
	if err := s.validateWebhookConfig(name); err != nil {
		return err
	}
	return nil
}

func (s Source) validateWebhookConfig(name string) error {
	if s.Webhook == nil {
		return nil
	}
	if strings.TrimSpace(s.Webhook.ListenAddr) == "" {
		return fmt.Errorf("source %s webhook listen addr 不能为空", name)
	}
	if path := strings.TrimSpace(s.Webhook.Path); path == "" || !strings.HasPrefix(path, "/") {
		return fmt.Errorf("source %s webhook path 必须以 / 开头", name)
	}
	if strings.TrimSpace(s.Webhook.SecretEnv) == "" {
		return fmt.Errorf("source %s webhook secret_env 不能为空", name)
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
	hasRequired := false
	for _, target := range r.Targets {
		if err := target.Validate(sinks); err != nil {
			return fmt.Errorf("route %s target 校验失败: %w", name, err)
		}
		if target.Required {
			hasRequired = true
		}
		key := target.Key()
		if _, exists := seen[key]; exists {
			return fmt.Errorf("route %s 存在重复 target: %s", name, key)
		}
		seen[key] = struct{}{}
	}
	if !hasRequired {
		return fmt.Errorf("route %s 至少需要一个 required target", name)
	}
	if r.DeleteSource.Enabled && len(r.DeleteSource.EligibleSinks) == 0 {
		return fmt.Errorf("route %s 启用 delete source 时必须声明 eligible sinks", name)
	}
	if r.DeleteSource.Enabled {
		for _, ref := range r.DeleteSource.EligibleSinks {
			if _, ok := sinks[strings.TrimSpace(ref)]; !ok {
				return fmt.Errorf("route %s delete source 引用了不存在的 sink: %s", name, ref)
			}
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
