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
