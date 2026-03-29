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

func TestResolveCredentialPlanRequiresTopology(t *testing.T) {
	t.Parallel()

	_, err := ResolveCredentialPlan(appconfig.Config{}, "")
	if err == nil || !strings.Contains(err.Error(), "topology path 未配置") {
		t.Fatalf("ResolveCredentialPlan() error = %v, want topology path error", err)
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
