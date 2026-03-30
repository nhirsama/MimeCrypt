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
				StatePath:    "/state/flow-sync-archive.json",
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
				StatePath:    "/state/flow-sync-archive.json",
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

func TestTopologyValidateStructureRejectsSourceDriverWithoutSourceCapability(t *testing.T) {
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
	if err == nil || err.Error() != "source archive 不支持 driver: file" {
		t.Fatalf("ValidateStructure() error = %v, want unsupported source driver", err)
	}
}

func TestTopologyValidateStructureRejectsCredentialRefOnLocalSink(t *testing.T) {
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
				StatePath:    "/state/flow-sync-archive.json",
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
	if err == nil || err.Error() != "sink local 的 driver file 不接受 credential_ref" {
		t.Fatalf("ValidateStructure() error = %v, want local sink credential_ref rejection", err)
	}
}

func TestTopologyValidateStructureRejectsDeleteSourceForSoftDeleteSource(t *testing.T) {
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
				StatePath:    "/state/flow-sync-archive.json",
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
	if err == nil || err.Error() != "route default 启用 delete source 时，source archive 的 driver graph 仅支持 soft delete" {
		t.Fatalf("ValidateStructure() error = %v, want soft delete rejection", err)
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

func TestTopologyValidateStructureRejectsWebhookNonPushMode(t *testing.T) {
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
	if err == nil || err.Error() != "source incoming 的 driver webhook 不支持 mode: poll" {
		t.Fatalf("ValidateStructure() error = %v, want webhook push mode rejection", err)
	}
}

func TestTopologyValidateStructureRejectsWebhookConfigOnNonWebhookDriver(t *testing.T) {
	t.Parallel()

	topology := Topology{
		Sources: map[string]Source{
			"archive": {
				Name:         "archive",
				Driver:       "imap",
				Mode:         "poll",
				StatePath:    "/state/flow-sync-archive.json",
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
	if err == nil || err.Error() != "source archive 的 driver imap 不接受 webhook 配置" {
		t.Fatalf("ValidateStructure() error = %v, want webhook config rejection", err)
	}
}
