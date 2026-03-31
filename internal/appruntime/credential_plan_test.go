package appruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
)

func TestResolveCredentialPlanFallsBackToBaseConfigWithoutTopology(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	plan, err := ResolveCredentialPlan(appconfig.Config{
		TopologyPath: appconfig.DefaultTopologyPath(stateDir),
		Auth: appconfig.AuthConfig{
			StateDir:   stateDir,
			TokenStore: "file",
		},
	}, "")
	if err != nil {
		t.Fatalf("ResolveCredentialPlan() error = %v", err)
	}
	if plan.CredentialName != "" {
		t.Fatalf("CredentialName = %q, want empty", plan.CredentialName)
	}
	if plan.Config.Auth.StateDir != stateDir {
		t.Fatalf("Auth.StateDir = %q, want %q", plan.Config.Auth.StateDir, stateDir)
	}
}

func TestResolveCredentialPlanPreservesMissingExplicitTopologyError(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	_, err := ResolveCredentialPlan(appconfig.Config{
		TopologyPath: filepath.Join(stateDir, "custom-topology.json"),
		Auth: appconfig.AuthConfig{
			StateDir:   stateDir,
			TokenStore: "file",
		},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "读取 topology 配置失败") {
		t.Fatalf("ResolveCredentialPlan() error = %v, want missing topology error", err)
	}
}

func TestResolveCredentialPlanRejectsNamedCredentialWithoutTopology(t *testing.T) {
	t.Parallel()

	_, err := ResolveCredentialPlan(appconfig.Config{}, "office-auth")
	if err == nil || !strings.Contains(err.Error(), "需要 topology 配置") {
		t.Fatalf("ResolveCredentialPlan() error = %v, want topology requirement", err)
	}
}

func TestResolveCredentialCommandPlanAllowsExplicitCredentialWithoutTopology(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	plan, err := ResolveCredentialCommandPlan(appconfig.Config{
		TopologyPath: filepath.Join(stateDir, "missing-topology.json"),
		Auth: appconfig.AuthConfig{
			StateDir:   stateDir,
			TokenStore: "file",
		},
	}, "office-auth")
	if err != nil {
		t.Fatalf("ResolveCredentialCommandPlan() error = %v", err)
	}
	if plan.CredentialName != "office-auth" {
		t.Fatalf("CredentialName = %q, want office-auth", plan.CredentialName)
	}
	if plan.Config.Auth.StateDir != filepath.Join(stateDir, "credentials", "office-auth") {
		t.Fatalf("Auth.StateDir = %q", plan.Config.Auth.StateDir)
	}
}

func TestResolveCredentialCommandPlanRestoresDriversFromLocalConfig(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	credentialStateDir := filepath.Join(stateDir, "credentials", "office-auth")
	if err := appconfig.SaveLocalConfig(credentialStateDir, appconfig.LocalConfig{
		RuntimeName: "oauth-device",
		AuthProfile: "imap+graph",
		Microsoft: &appconfig.MicrosoftLocalConfig{
			IMAPUsername: "stored@example.com",
		},
	}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	plan, err := ResolveCredentialCommandPlan(appconfig.Config{
		TopologyPath: filepath.Join(stateDir, "missing-topology.json"),
		Auth: appconfig.AuthConfig{
			StateDir:   stateDir,
			TokenStore: "file",
		},
	}, "office-auth")
	if err != nil {
		t.Fatalf("ResolveCredentialCommandPlan() error = %v", err)
	}
	if got := strings.Join(plan.RuntimeAuthHints(), ","); got != "graph,imap" {
		t.Fatalf("RuntimeAuthHints = %q, want graph,imap", got)
	}
	if plan.LocalConfig.EffectiveRuntimeName() != "oauth-device" {
		t.Fatalf("LocalConfig.EffectiveRuntimeName = %q, want oauth-device", plan.LocalConfig.EffectiveRuntimeName())
	}
	if plan.LocalConfig.EffectiveAuthProfile() != "graph+imap" {
		t.Fatalf("LocalConfig.EffectiveAuthProfile = %q, want graph+imap", plan.LocalConfig.EffectiveAuthProfile())
	}
	if plan.Config.Mail.Client.IMAPUsername != "stored@example.com" {
		t.Fatalf("IMAPUsername = %q, want stored@example.com", plan.Config.Mail.Client.IMAPUsername)
	}
}

func TestResolveCredentialCommandPlanKeepsStoredDriversWhenTopologyHasBindings(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	writeCredentialTopology(t, topologyPath, appconfig.Topology{
		DefaultCredential: "office-auth",
		Credentials: map[string]appconfig.Credential{
			"office-auth": {Name: "office-auth", Kind: "oauth"},
		},
		Sources: map[string]appconfig.Source{
			"office": {
				Name:          "office",
				Driver:        "imap",
				Mode:          "poll",
				CredentialRef: "office-auth",
				Folder:        "INBOX",
				PollInterval:  time.Minute,
				CycleTimeout:  2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]appconfig.Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"office"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
	})

	credentialStateDir := filepath.Join(stateDir, "credentials", "office-auth")
	if err := appconfig.SaveLocalConfig(credentialStateDir, appconfig.LocalConfig{
		AuthProfile: "graph+imap",
		Microsoft: &appconfig.MicrosoftLocalConfig{
			IMAPUsername: "stored@example.com",
		},
	}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	plan, err := ResolveCredentialCommandPlan(appconfig.Config{
		TopologyPath: topologyPath,
		Auth: appconfig.AuthConfig{
			StateDir:   stateDir,
			TokenStore: "file",
		},
	}, "office-auth")
	if err != nil {
		t.Fatalf("ResolveCredentialCommandPlan() error = %v", err)
	}
	if got := strings.Join(plan.RuntimeAuthHints(), ","); got != "graph,imap" {
		t.Fatalf("RuntimeAuthHints = %q, want graph,imap", got)
	}
	if len(plan.Bindings) != 1 || plan.Bindings[0].Driver != "imap" {
		t.Fatalf("Bindings = %+v, want single imap binding", plan.Bindings)
	}
	if plan.Config.Mail.Client.IMAPUsername != "stored@example.com" {
		t.Fatalf("IMAPUsername = %q, want stored@example.com", plan.Config.Mail.Client.IMAPUsername)
	}
}

func TestResolveCredentialCommandPlanCollectsBindingsWithoutInferringDrivers(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	writeCredentialTopology(t, topologyPath, appconfig.Topology{
		DefaultCredential: "office-auth",
		Credentials: map[string]appconfig.Credential{
			"office-auth": {Name: "office-auth", Kind: "oauth"},
		},
		Sources: map[string]appconfig.Source{
			"office": {
				Name:          "office",
				Driver:        "imap",
				Mode:          "poll",
				CredentialRef: "office-auth",
				Folder:        "INBOX",
				PollInterval:  time.Minute,
				CycleTimeout:  2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]appconfig.Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"office"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
	})

	plan, err := ResolveCredentialCommandPlan(appconfig.Config{
		TopologyPath: topologyPath,
		Auth: appconfig.AuthConfig{
			StateDir:   stateDir,
			TokenStore: "file",
		},
	}, "office-auth")
	if err != nil {
		t.Fatalf("ResolveCredentialCommandPlan() error = %v", err)
	}
	if len(plan.Bindings) != 1 || plan.Bindings[0].Driver != "imap" {
		t.Fatalf("Bindings = %+v, want single imap binding", plan.Bindings)
	}
	if len(plan.RuntimeAuthHints()) != 0 {
		t.Fatalf("RuntimeAuthHints = %#v, want empty without local runtime config", plan.RuntimeAuthHints())
	}
}

func TestResolveCredentialCommandPlanFallsBackWhenTopologyIsInvalid(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	if err := os.WriteFile(topologyPath, []byte("{invalid json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	plan, err := ResolveCredentialCommandPlan(appconfig.Config{
		TopologyPath: topologyPath,
		Auth: appconfig.AuthConfig{
			StateDir:   stateDir,
			TokenStore: "file",
		},
	}, "office-auth")
	if err != nil {
		t.Fatalf("ResolveCredentialCommandPlan() error = %v", err)
	}
	if plan.CredentialName != "office-auth" {
		t.Fatalf("CredentialName = %q, want office-auth", plan.CredentialName)
	}
	if len(plan.Bindings) != 0 {
		t.Fatalf("Bindings = %+v, want empty bootstrap bindings", plan.Bindings)
	}
}

func TestResolveCredentialPlanUsesExplicitCredentialFromTopology(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	writeCredentialTopology(t, topologyPath, appconfig.Topology{
		DefaultCredential: "default",
		Credentials: map[string]appconfig.Credential{
			"default": {
				Name:       "default",
				Kind:       "oauth",
				IMAPScopes: []string{"imap.default"},
			},
			"archive-auth": {
				Name:       "archive-auth",
				Kind:       "oauth",
				TokenStore: "keyring",
				IMAPScopes: []string{"imap.archive"},
			},
		},
		Sources: map[string]appconfig.Source{
			"default": {
				Name:         "default",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "INBOX",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]appconfig.Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"default"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
	})

	plan, err := ResolveCredentialPlan(appconfig.Config{
		TopologyPath: topologyPath,
		Auth: appconfig.AuthConfig{
			StateDir:   stateDir,
			TokenStore: "file",
		},
	}, "archive-auth")
	if err != nil {
		t.Fatalf("ResolveCredentialPlan() error = %v", err)
	}
	if plan.CredentialName != "archive-auth" {
		t.Fatalf("CredentialName = %q, want archive-auth", plan.CredentialName)
	}
	if plan.Config.Auth.TokenStore != "keyring" {
		t.Fatalf("TokenStore = %q, want keyring", plan.Config.Auth.TokenStore)
	}
	if got := plan.Config.Mail.Sync.StateDir; got != filepath.Join(stateDir, "credentials", "archive-auth") {
		t.Fatalf("Mail.Sync.StateDir = %q", got)
	}
}

func TestResolveCredentialPlanAllowsTopologyWithoutNamedCredentials(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	writeCredentialTopology(t, topologyPath, appconfig.Topology{
		Sources: map[string]appconfig.Source{
			"default": {
				Name:         "default",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "INBOX",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]appconfig.Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"default"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
	})

	plan, err := ResolveCredentialPlan(appconfig.Config{
		TopologyPath: topologyPath,
		Auth: appconfig.AuthConfig{
			StateDir: stateDir,
		},
	}, "")
	if err != nil {
		t.Fatalf("ResolveCredentialPlan() error = %v", err)
	}
	if plan.CredentialName != "" {
		t.Fatalf("CredentialName = %q, want empty", plan.CredentialName)
	}
	if plan.Config.Auth.StateDir != stateDir {
		t.Fatalf("Auth.StateDir = %q, want %q", plan.Config.Auth.StateDir, stateDir)
	}
}

func TestResolveCredentialPlanAllowsExplicitScopeClearFromTopology(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	if err := os.WriteFile(topologyPath, []byte(`{
  "default_credential": "imap-only",
  "credentials": {
    "imap-only": {
      "name": "imap-only",
      "kind": "oauth",
      "graph_scopes": [],
      "imap_scopes": ["scope-imap"],
      "imap_username": "imap@example.com"
    }
  },
  "sources": {
    "default": {
      "name": "default",
      "driver": "imap",
      "mode": "poll",
      "folder": "INBOX",
      "poll_interval": 60000000000,
      "cycle_timeout": 120000000000
    }
  },
  "sinks": {
    "discard": {"name": "discard", "driver": "discard"}
  },
  "routes": {
    "default": {
      "name": "default",
      "source_refs": ["default"],
      "targets": [{"name": "discard", "sink_ref": "discard", "artifact": "primary", "required": true}]
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	plan, err := ResolveCredentialPlan(appconfig.Config{
		TopologyPath: topologyPath,
		Auth: appconfig.AuthConfig{
			StateDir:       stateDir,
			TokenStore:     "file",
			GraphScopes:    []string{"scope-graph"},
			IMAPScopes:     []string{"scope-imap-base"},
			ClientID:       "client-id",
			Tenant:         "organizations",
			KeyringService: "mimecrypt",
		},
	}, "")
	if err != nil {
		t.Fatalf("ResolveCredentialPlan() error = %v", err)
	}
	if len(plan.Config.Auth.GraphScopes) != 0 {
		t.Fatalf("GraphScopes = %#v, want empty", plan.Config.Auth.GraphScopes)
	}
	if !strings.EqualFold(strings.Join(plan.Config.Auth.IMAPScopes, " "), "scope-imap") {
		t.Fatalf("IMAPScopes = %#v, want [scope-imap]", plan.Config.Auth.IMAPScopes)
	}
	if plan.Config.Mail.Client.IMAPUsername != "imap@example.com" {
		t.Fatalf("IMAPUsername = %q, want imap@example.com", plan.Config.Mail.Client.IMAPUsername)
	}
}

func TestResolveCredentialPlanCollectsBindingsAndBindingDrivers(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	writeCredentialTopology(t, topologyPath, appconfig.Topology{
		DefaultCredential: "shared",
		Credentials: map[string]appconfig.Credential{
			"shared": {Name: "shared", Kind: "oauth"},
			"other":  {Name: "other", Kind: "oauth"},
		},
		Sources: map[string]appconfig.Source{
			"inbox": {
				Name:         "inbox",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "INBOX",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
			"other-source": {
				Name:          "other-source",
				Driver:        "graph",
				Mode:          "poll",
				CredentialRef: "other",
				PollInterval:  time.Minute,
				CycleTimeout:  2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"vault": {Name: "vault", Driver: "graph"},
			"copy":  {Name: "copy", Driver: "imap", CredentialRef: "shared"},
			"local": {Name: "local", Driver: "file", OutputDir: filepath.Join(stateDir, "out")},
		},
		Routes: map[string]appconfig.Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"inbox"},
				Targets: []appconfig.RouteTarget{
					{Name: "local", SinkRef: "local", Artifact: "primary", Required: true},
				},
			},
		},
	})

	plan, err := ResolveCredentialPlan(appconfig.Config{
		TopologyPath: topologyPath,
		Auth: appconfig.AuthConfig{
			StateDir:   stateDir,
			TokenStore: "file",
		},
	}, "shared")
	if err != nil {
		t.Fatalf("ResolveCredentialPlan() error = %v", err)
	}

	if got := strings.Join(plan.BindingDrivers, ","); got != "graph,imap" {
		t.Fatalf("BindingDrivers = %q, want graph,imap", got)
	}

	wantBindings := []CredentialBinding{
		{Kind: CredentialBindingSource, Name: "inbox", Driver: "imap", Implicit: true},
		{Kind: CredentialBindingSink, Name: "copy", Driver: "imap", Implicit: false},
		{Kind: CredentialBindingSink, Name: "vault", Driver: "graph", Implicit: true},
	}
	if len(plan.Bindings) != len(wantBindings) {
		t.Fatalf("len(Bindings) = %d, want %d", len(plan.Bindings), len(wantBindings))
	}
	for idx, want := range wantBindings {
		if plan.Bindings[idx] != want {
			t.Fatalf("Bindings[%d] = %+v, want %+v", idx, plan.Bindings[idx], want)
		}
	}
}

func TestResolveCredentialPlanIgnoresAmbiguousImplicitBindingsWhenCredentialExplicitlySelected(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	writeCredentialTopology(t, topologyPath, appconfig.Topology{
		Credentials: map[string]appconfig.Credential{
			"shared": {Name: "shared", Kind: "oauth"},
			"other":  {Name: "other", Kind: "oauth"},
		},
		Sources: map[string]appconfig.Source{
			"ambiguous": {
				Name:         "ambiguous",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "INBOX",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"copy": {Name: "copy", Driver: "imap", CredentialRef: "shared"},
		},
		Routes: map[string]appconfig.Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"ambiguous"},
				Targets: []appconfig.RouteTarget{
					{Name: "copy", SinkRef: "copy", Artifact: "primary", Required: true},
				},
			},
		},
	})

	plan, err := ResolveCredentialPlan(appconfig.Config{
		TopologyPath: topologyPath,
		Auth: appconfig.AuthConfig{
			StateDir:   stateDir,
			TokenStore: "file",
		},
	}, "shared")
	if err != nil {
		t.Fatalf("ResolveCredentialPlan() error = %v", err)
	}

	if got := strings.Join(plan.BindingDrivers, ","); got != "imap" {
		t.Fatalf("BindingDrivers = %q, want imap", got)
	}
	if len(plan.Bindings) != 1 || plan.Bindings[0].Name != "copy" {
		t.Fatalf("Bindings = %+v, want only explicit sink binding", plan.Bindings)
	}
}

func writeCredentialTopology(t *testing.T, path string, topology appconfig.Topology) {
	t.Helper()
	content, err := json.Marshal(topology)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
