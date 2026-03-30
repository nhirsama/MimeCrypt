package appconfig

import (
	"testing"
	"time"
)

func TestTopologyValidateStructureAllowsMissingDefaultSelections(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Credentials: map[string]Credential{
			"archive-auth": {Name: "archive-auth", Kind: "oauth"},
		},
		Sources: map[string]Source{
			"archive": {
				Name:         "archive",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "Archive",
				PollInterval: 1,
				CycleTimeout: 1,
			},
		},
		Sinks: map[string]Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"archive"},
				Targets: []RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
	}

	if err := topology.ValidateStructure(); err != nil {
		t.Fatalf("ValidateStructure() error = %v", err)
	}
	if err := topology.Validate(); err == nil || err.Error() != "default source 未配置" {
		t.Fatalf("Validate() error = %v, want default source error", err)
	}
}

func TestTopologyValidateStructureRejectsInvalidConfiguredDefaultSelection(t *testing.T) {
	t.Parallel()

	topology := Topology{
		DefaultSource: "missing",
		Sources: map[string]Source{
			"archive": {
				Name:         "archive",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "Archive",
				PollInterval: 1,
				CycleTimeout: 1,
			},
		},
		Sinks: map[string]Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"archive"},
				Targets: []RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
	}

	if err := topology.ValidateStructure(); err == nil || err.Error() != "default source 不存在: missing" {
		t.Fatalf("ValidateStructure() error = %v, want invalid default source", err)
	}
}

func TestTopologyResolveCredentialRefFallsBackToSingleCredential(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Credentials: map[string]Credential{
			"archive-auth": {Name: "archive-auth", Kind: "oauth"},
		},
	}

	got, err := topology.ResolveCredentialRef("")
	if err != nil {
		t.Fatalf("ResolveCredentialRef() error = %v", err)
	}
	if got != "archive-auth" {
		t.Fatalf("ResolveCredentialRef() = %q, want archive-auth", got)
	}
}

func TestTopologyResolveCredentialRefRejectsUnknownExplicitCredential(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Credentials: map[string]Credential{
			"default": {Name: "default", Kind: "oauth"},
		},
	}

	_, err := topology.ResolveCredentialRef("missing")
	if err == nil || err.Error() != "credential 不存在: missing" {
		t.Fatalf("ResolveCredentialRef() error = %v, want missing credential error", err)
	}
}

func TestTopologyValidateStructureRejectsUnknownCredentialKind(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Credentials: map[string]Credential{
			"default": {Name: "default", Kind: "mystery"},
		},
		Sources: map[string]Source{
			"incoming": {
				Name:   "incoming",
				Driver: "webhook",
				Mode:   "push",
				Webhook: &WebhookSource{
					ListenAddr: "127.0.0.1:8080",
					Path:       "/mail/incoming",
					SecretEnv:  "MIMECRYPT_WEBHOOK_SECRET",
				},
			},
		},
		Sinks: map[string]Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"incoming"},
				Targets: []RouteTarget{
					{Name: "discard", SinkRef: "discard", Required: true},
				},
			},
		},
	}

	err := topology.ValidateStructure()
	if err == nil || err.Error() != "credential default 不支持 kind: mystery" {
		t.Fatalf("ValidateStructure() error = %v, want unknown credential kind rejection", err)
	}
}

func TestTopologyValidateStructureAllowsUnknownSourceDriverForRuntimeValidation(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Sources: map[string]Source{
			"archive": {
				Name:   "archive",
				Driver: "file",
				Mode:   "poll",
			},
		},
		Sinks: map[string]Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"archive"},
				Targets: []RouteTarget{
					{Name: "discard", SinkRef: "discard", Required: true},
				},
			},
		},
	}

	err := topology.ValidateStructure()
	if err != nil {
		t.Fatalf("ValidateStructure() error = %v", err)
	}
}

func TestTopologyValidateStructureAllowsCredentialRefOnAnySinkForRuntimeValidation(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Credentials: map[string]Credential{
			"default": {Name: "default", Kind: "oauth"},
		},
		Sources: map[string]Source{
			"archive": {
				Name:         "archive",
				Driver:       "imap",
				Mode:         "poll",
				PollInterval: 1,
				CycleTimeout: 1,
			},
		},
		Sinks: map[string]Sink{
			"local": {Name: "local", Driver: "file", OutputDir: "/tmp/out", CredentialRef: "default"},
		},
		Routes: map[string]Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"archive"},
				Targets: []RouteTarget{
					{Name: "local", SinkRef: "local", Required: true},
				},
			},
		},
	}

	err := topology.ValidateStructure()
	if err != nil {
		t.Fatalf("ValidateStructure() error = %v", err)
	}
}

func TestTopologyValidateStructureAllowsDeleteSourceWithoutDriverCapabilityCheck(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Credentials: map[string]Credential{
			"default": {Name: "default", Kind: "oauth"},
		},
		Sources: map[string]Source{
			"archive": {
				Name:         "archive",
				Driver:       "graph",
				Mode:         "poll",
				PollInterval: 1,
				CycleTimeout: 1,
			},
		},
		Sinks: map[string]Sink{
			"remote": {Name: "remote", Driver: "graph"},
		},
		Routes: map[string]Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"archive"},
				Targets: []RouteTarget{
					{Name: "remote", SinkRef: "remote", Required: true},
				},
				DeleteSource: DeleteSourcePolicy{
					Enabled:       true,
					EligibleSinks: []string{"remote"},
				},
			},
		},
	}

	err := topology.ValidateStructure()
	if err != nil {
		t.Fatalf("ValidateStructure() error = %v", err)
	}
}

func TestConfiguredInstanceSummaryPreservesDeclaredIdentity(t *testing.T) {
	t.Parallel()

	source := Source{
		Name:          "incoming",
		Driver:        "webhook",
		Mode:          "push",
		CredentialRef: "default-auth",
	}
	sink := Sink{
		Name:          "archive",
		Driver:        "imap",
		CredentialRef: "default-auth",
	}
	credential := Credential{
		Name: "default-auth",
		Kind: CredentialKindOAuth,
	}

	if got := source.Summary(); got.Kind != InstanceKindSource || got.Name != "incoming" || got.Driver != "webhook" || got.CredentialRef != "default-auth" {
		t.Fatalf("source summary = %+v", got)
	}
	if got := sink.Summary(); got.Kind != InstanceKindSink || got.Name != "archive" || got.Driver != "imap" || got.CredentialRef != "default-auth" {
		t.Fatalf("sink summary = %+v", got)
	}
	if got := credential.Summary(); got.Kind != InstanceKindCredential || got.Name != "default-auth" || got.Driver != CredentialKindOAuth {
		t.Fatalf("credential summary = %+v", got)
	}
}

func TestTopologyNamedInstanceLookupsUseConfiguredInstanceSemantics(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Sources: map[string]Source{
			"incoming": {Name: "incoming", Driver: "webhook", Mode: "push", StatePath: "/runtime/source.json"},
		},
		Sinks: map[string]Sink{
			"archive": {Name: "archive", Driver: "file", OutputDir: "/tmp/out"},
		},
		Routes: map[string]Route{
			"default": {Name: "default", StateDir: "/runtime/route"},
		},
	}

	if source, err := topology.SourceInstance("incoming"); err != nil || source.Name != "incoming" {
		t.Fatalf("SourceInstance() = %+v, %v", source, err)
	} else if source.StatePath != "" {
		t.Fatalf("SourceInstance() preserved runtime state path = %q", source.StatePath)
	}
	if sink, err := topology.SinkInstance("archive"); err != nil || sink.Name != "archive" {
		t.Fatalf("SinkInstance() = %+v, %v", sink, err)
	}
	if route, err := topology.RouteInstance("default"); err != nil || route.Name != "default" {
		t.Fatalf("RouteInstance() = %+v, %v", route, err)
	} else if route.StateDir != "" {
		t.Fatalf("RouteInstance() preserved runtime state dir = %q", route.StateDir)
	}
}

func TestTopologyNormalizeDropsRuntimeDerivedFields(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Sources: map[string]Source{
			"archive": {Driver: "imap", Mode: "poll", StatePath: "/runtime/source.json"},
		},
		Routes: map[string]Route{
			"default": {SourceRefs: []string{"archive"}, StateDir: "/runtime/route"},
		},
	}

	normalized := topology.Normalize()
	if got := normalized.Sources["archive"].StatePath; got != "" {
		t.Fatalf("normalized source state path = %q, want empty", got)
	}
	if got := normalized.Routes["default"].StateDir; got != "" {
		t.Fatalf("normalized route state dir = %q, want empty", got)
	}
}

func TestTopologyValidateStructureAllowsWebhookPushSource(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Sources: map[string]Source{
			"incoming": {
				Name:   "incoming",
				Driver: "webhook",
				Mode:   "push",
				Webhook: &WebhookSource{
					ListenAddr: "127.0.0.1:8080",
					Path:       "/mail/incoming",
					SecretEnv:  "MIMECRYPT_WEBHOOK_SECRET",
				},
			},
		},
		Sinks: map[string]Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"incoming"},
				Targets: []RouteTarget{
					{Name: "discard", SinkRef: "discard", Required: true},
				},
			},
		},
	}

	if err := topology.ValidateStructure(); err != nil {
		t.Fatalf("ValidateStructure() error = %v", err)
	}
}

func TestTopologyValidateStructureRejectsRouteWithoutRequiredTarget(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Sources: map[string]Source{
			"incoming": {
				Name:   "incoming",
				Driver: "webhook",
				Mode:   "push",
				Webhook: &WebhookSource{
					ListenAddr: "127.0.0.1:8080",
					Path:       "/mail/incoming",
					SecretEnv:  "MIMECRYPT_WEBHOOK_SECRET",
				},
			},
		},
		Sinks: map[string]Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"incoming"},
				Targets: []RouteTarget{
					{Name: "discard", SinkRef: "discard"},
				},
			},
		},
	}

	err := topology.ValidateStructure()
	if err == nil || err.Error() != "route default 至少需要一个 required target" {
		t.Fatalf("ValidateStructure() error = %v, want required target error", err)
	}
}

func TestTopologyValidateStructureAllowsWebhookDriverNonPushModeForRuntimeValidation(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Sources: map[string]Source{
			"incoming": {
				Name:   "incoming",
				Driver: "webhook",
				Mode:   "poll",
				Webhook: &WebhookSource{
					ListenAddr: "127.0.0.1:8080",
					Path:       "/mail/incoming",
					SecretEnv:  "MIMECRYPT_WEBHOOK_SECRET",
				},
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
		},
		Sinks: map[string]Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"incoming"},
				Targets: []RouteTarget{
					{Name: "discard", SinkRef: "discard", Required: true},
				},
			},
		},
	}

	err := topology.ValidateStructure()
	if err != nil {
		t.Fatalf("ValidateStructure() error = %v", err)
	}
}

func TestTopologyValidateStructureAllowsWebhookConfigOnNonWebhookDriverForRuntimeValidation(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Sources: map[string]Source{
			"archive": {
				Name:         "archive",
				Driver:       "imap",
				Mode:         "poll",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
				Webhook: &WebhookSource{
					ListenAddr: "127.0.0.1:8080",
					Path:       "/mail/incoming",
					SecretEnv:  "MIMECRYPT_WEBHOOK_SECRET",
				},
			},
		},
		Sinks: map[string]Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"archive"},
				Targets: []RouteTarget{
					{Name: "discard", SinkRef: "discard", Required: true},
				},
			},
		},
	}

	err := topology.ValidateStructure()
	if err != nil {
		t.Fatalf("ValidateStructure() error = %v", err)
	}
}
