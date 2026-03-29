package flowruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
)

func TestResolveRoutePlanAllSourcesUsesCredentialScopedStateLayout(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	topology := appconfig.Topology{
		DefaultRoute: "archive",
		Credentials: map[string]appconfig.Credential{
			"default": {
				Name: "default",
				Kind: "shared-session",
			},
			"vault-auth": {
				Name: "vault-auth",
				Kind: "oauth",
			},
		},
		Sources: map[string]appconfig.Source{
			"office": {
				Name:          "office",
				Driver:        "imap",
				Mode:          "poll",
				CredentialRef: "default",
				Folder:        "Inbox",
				PollInterval:  time.Minute,
				CycleTimeout:  2 * time.Minute,
			},
			"vault": {
				Name:          "vault",
				Driver:        "imap",
				Mode:          "poll",
				CredentialRef: "vault-auth",
				Folder:        "Archive/Sub",
				PollInterval:  2 * time.Minute,
				CycleTimeout:  3 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"local-output": {
				Name:      "local-output",
				Driver:    "file",
				OutputDir: filepath.Join(stateDir, "output"),
			},
			"write-back": {
				Name:          "write-back",
				Driver:        "imap",
				CredentialRef: "vault-auth",
				Folder:        "Encrypted",
			},
		},
		Routes: map[string]appconfig.Route{
			"archive": {
				Name:       "archive",
				SourceRefs: []string{"office", "vault"},
				Targets: []appconfig.RouteTarget{
					{Name: "local-output", SinkRef: "local-output", Artifact: "primary", Required: true},
					{Name: "write-back", SinkRef: "write-back", Artifact: "primary", Required: false},
				},
			},
		},
	}
	writeTopologyFile(t, topologyPath, topology)

	plan, err := ResolveRoutePlan(testRuntimeConfig(stateDir, topologyPath), Selector{RouteName: "archive"}, appconfig.TopologyOptions{}, RoutePlanAllSources)
	if err != nil {
		t.Fatalf("ResolveRoutePlan() error = %v", err)
	}
	if len(plan.Runs) != 2 {
		t.Fatalf("len(Runs) = %d, want 2", len(plan.Runs))
	}

	runs := map[string]SourceRun{}
	for _, run := range plan.Runs {
		runs[run.Source.Name] = run
	}

	office := runs["office"]
	if office.Source.StatePath != filepath.Join(stateDir, "flow-sync-office-imap-Inbox.json") {
		t.Fatalf("office source state path = %q", office.Source.StatePath)
	}
	if office.Route.StateDir != filepath.Join(stateDir, "flow-state", "archive-office-imap-Inbox") {
		t.Fatalf("office route state dir = %q", office.Route.StateDir)
	}

	vaultStateDir := filepath.Join(stateDir, "credentials", "vault-auth")
	vault := runs["vault"]
	if vault.Config.Auth.StateDir != vaultStateDir {
		t.Fatalf("vault Auth.StateDir = %q, want %q", vault.Config.Auth.StateDir, vaultStateDir)
	}
	if vault.Config.Mail.Sync.StateDir != vaultStateDir {
		t.Fatalf("vault Mail.Sync.StateDir = %q, want %q", vault.Config.Mail.Sync.StateDir, vaultStateDir)
	}
	if vault.Source.StatePath != filepath.Join(vaultStateDir, "flow-sync-vault-imap-Archive_Sub.json") {
		t.Fatalf("vault source state path = %q", vault.Source.StatePath)
	}
	if vault.Route.StateDir != filepath.Join(vaultStateDir, "flow-state", "archive-vault-imap-Archive_Sub") {
		t.Fatalf("vault route state dir = %q", vault.Route.StateDir)
	}
	if sink, ok := vault.Sinks["write-back"]; !ok {
		t.Fatalf("missing write-back sink plan")
	} else if sink.Config.Auth.StateDir != vaultStateDir {
		t.Fatalf("write-back Auth.StateDir = %q, want %q", sink.Config.Auth.StateDir, vaultStateDir)
	}
}

func TestResolveRoutePlanSingleSourceRejectsAmbiguousRouteWithoutSelection(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	topology := appconfig.Topology{
		DefaultRoute: "archive",
		Credentials: map[string]appconfig.Credential{
			"default": {Name: "default", Kind: "shared-session"},
		},
		Sources: map[string]appconfig.Source{
			"office": {
				Name:         "office",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "Inbox",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
			"mirror": {
				Name:         "mirror",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "Mirror",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]appconfig.Route{
			"archive": {
				Name:       "archive",
				SourceRefs: []string{"office", "mirror"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
	}
	writeTopologyFile(t, topologyPath, topology)

	_, err := ResolveRoutePlan(testRuntimeConfig(stateDir, topologyPath), Selector{RouteName: "archive"}, appconfig.TopologyOptions{}, RoutePlanSingleSource)
	if err == nil || !strings.Contains(err.Error(), "显式指定 --source") {
		t.Fatalf("ResolveRoutePlan() error = %v, want explicit source selection", err)
	}
}

func TestResolveSourcePlanUsesCredentialScopedConfigAndStatePath(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	topology := appconfig.Topology{
		Credentials: map[string]appconfig.Credential{
			"default":      {Name: "default", Kind: "shared-session"},
			"archive-auth": {Name: "archive-auth", Kind: "oauth"},
		},
		Sources: map[string]appconfig.Source{
			"archive": {
				Name:          "archive",
				Driver:        "imap",
				Mode:          "poll",
				CredentialRef: "archive-auth",
				Folder:        "Archive/2026",
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
				SourceRefs: []string{"archive"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
	}
	writeTopologyFile(t, topologyPath, topology)

	plan, err := ResolveSourcePlan(testRuntimeConfig(stateDir, topologyPath), Selector{})
	if err != nil {
		t.Fatalf("ResolveSourcePlan() error = %v", err)
	}

	credentialStateDir := filepath.Join(stateDir, "credentials", "archive-auth")
	if plan.Config.Auth.StateDir != credentialStateDir {
		t.Fatalf("Auth.StateDir = %q, want %q", plan.Config.Auth.StateDir, credentialStateDir)
	}
	if plan.Config.Mail.Sync.StateDir != credentialStateDir {
		t.Fatalf("Mail.Sync.StateDir = %q, want %q", plan.Config.Mail.Sync.StateDir, credentialStateDir)
	}
	if plan.Source.StatePath != filepath.Join(credentialStateDir, "flow-sync-archive-imap-Archive_2026.json") {
		t.Fatalf("Source.StatePath = %q", plan.Source.StatePath)
	}
}

func TestResolveSourcePlanFallsBackToSingleCredentialWhenCredentialRefEmpty(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	topology := appconfig.Topology{
		Credentials: map[string]appconfig.Credential{
			"archive-auth": {Name: "archive-auth", Kind: "oauth"},
		},
		Sources: map[string]appconfig.Source{
			"archive": {
				Name:         "archive",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "Archive/2026",
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
				SourceRefs: []string{"archive"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
	}
	writeTopologyFile(t, topologyPath, topology)

	plan, err := ResolveSourcePlan(testRuntimeConfig(stateDir, topologyPath), Selector{})
	if err != nil {
		t.Fatalf("ResolveSourcePlan() error = %v", err)
	}

	credentialStateDir := filepath.Join(stateDir, "credentials", "archive-auth")
	if plan.Config.Auth.StateDir != credentialStateDir {
		t.Fatalf("Auth.StateDir = %q, want %q", plan.Config.Auth.StateDir, credentialStateDir)
	}
}

func TestResolveRoutePlanUsesDefaultCredentialForSinkWithoutCredentialRef(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	topology := appconfig.Topology{
		DefaultCredential: "archive-auth",
		Credentials: map[string]appconfig.Credential{
			"default":      {Name: "default", Kind: "shared-session"},
			"archive-auth": {Name: "archive-auth", Kind: "oauth"},
		},
		Sources: map[string]appconfig.Source{
			"office": {
				Name:          "office",
				Driver:        "imap",
				Mode:          "poll",
				CredentialRef: "default",
				Folder:        "Inbox",
				PollInterval:  time.Minute,
				CycleTimeout:  2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"archive": {
				Name:   "archive",
				Driver: "imap",
				Folder: "Encrypted",
			},
		},
		Routes: map[string]appconfig.Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"office"},
				Targets: []appconfig.RouteTarget{
					{Name: "archive", SinkRef: "archive", Artifact: "primary", Required: true},
				},
			},
		},
	}
	writeTopologyFile(t, topologyPath, topology)

	plan, err := ResolveRoutePlan(testRuntimeConfig(stateDir, topologyPath), Selector{}, appconfig.TopologyOptions{}, RoutePlanSingleSource)
	if err != nil {
		t.Fatalf("ResolveRoutePlan() error = %v", err)
	}
	if len(plan.Runs) != 1 {
		t.Fatalf("len(Runs) = %d, want 1", len(plan.Runs))
	}

	sink, ok := plan.Runs[0].Sinks["archive"]
	if !ok {
		t.Fatalf("missing archive sink")
	}
	wantStateDir := filepath.Join(stateDir, "credentials", "archive-auth")
	if sink.Config.Auth.StateDir != wantStateDir {
		t.Fatalf("sink Auth.StateDir = %q, want %q", sink.Config.Auth.StateDir, wantStateDir)
	}
}

func TestResolveSourcePlanRejectsAmbiguousImplicitCredential(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	topology := appconfig.Topology{
		Credentials: map[string]appconfig.Credential{
			"office-auth":  {Name: "office-auth", Kind: "oauth"},
			"archive-auth": {Name: "archive-auth", Kind: "oauth"},
		},
		Sources: map[string]appconfig.Source{
			"archive": {
				Name:         "archive",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "Archive/2026",
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
				SourceRefs: []string{"archive"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
	}
	writeTopologyFile(t, topologyPath, topology)

	_, err := ResolveSourcePlan(testRuntimeConfig(stateDir, topologyPath), Selector{})
	if err == nil || !strings.Contains(err.Error(), "显式设置 credential_ref 或 default_credential") {
		t.Fatalf("ResolveSourcePlan() error = %v, want ambiguous credential error", err)
	}
}

func testRuntimeConfig(stateDir, topologyPath string) appconfig.Config {
	return appconfig.Config{
		Provider:     "imap",
		TopologyPath: topologyPath,
		Auth: appconfig.AuthConfig{
			ClientID:         "client-id",
			Tenant:           "organizations",
			AuthorityBaseURL: "https://login.microsoftonline.com",
			IMAPScopes:       []string{"imap.read"},
			StateDir:         stateDir,
			TokenStore:       "file",
		},
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{
				IMAPAddr:     "imap.example.com:993",
				IMAPUsername: "user@example.com",
				GraphBaseURL: "https://graph.example.com/v1.0",
				EWSBaseURL:   "https://ews.example.com/EWS/Exchange.asmx",
			},
			Pipeline: appconfig.MailPipelineConfig{
				AuditLogPath: appconfig.DefaultAuditLogPath(stateDir),
			},
			Sync: appconfig.MailSyncConfig{
				StateDir: stateDir,
			},
		},
	}
}

func writeTopologyFile(t *testing.T, path string, topology appconfig.Topology) {
	t.Helper()
	content, err := json.Marshal(topology)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
