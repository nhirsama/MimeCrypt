package appruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mimecrypt/internal/appconfig"
)

func TestResolveCredentialPlanLegacyRejectsNamedCredential(t *testing.T) {
	t.Parallel()

	_, err := ResolveCredentialPlan(appconfig.Config{}, "archive-auth")
	if err == nil || !strings.Contains(err.Error(), "legacy 模式只支持 credential=default") {
		t.Fatalf("ResolveCredentialPlan() error = %v, want legacy selection error", err)
	}
}

func TestResolveCredentialPlanUsesExplicitCredentialFromTopology(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	content, err := json.Marshal(appconfig.Topology{
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
				StatePath:    filepath.Join(stateDir, "flow-sync-default.json"),
				PollInterval: 1,
				CycleTimeout: 1,
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
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(topologyPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

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
	if !plan.Custom {
		t.Fatalf("Custom = false, want true")
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
	content, err := json.Marshal(appconfig.Topology{
		Sources: map[string]appconfig.Source{
			"default": {
				Name:         "default",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "INBOX",
				StatePath:    filepath.Join(stateDir, "flow-sync-default.json"),
				PollInterval: 1,
				CycleTimeout: 1,
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
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(topologyPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

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
	if plan.Custom {
		t.Fatalf("Custom = true, want false")
	}
	if plan.Config.Auth.StateDir != stateDir {
		t.Fatalf("Auth.StateDir = %q, want %q", plan.Config.Auth.StateDir, stateDir)
	}
}
