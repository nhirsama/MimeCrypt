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
			"default":    {Name: "default", Kind: "shared-session"},
			"vault-auth": {Name: "vault-auth", Kind: "oauth"},
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

	plan, err := ResolveRoutePlan(testRuntimeConfig(stateDir, topologyPath), Selector{RouteName: "archive"}, RoutePlanAllSources)
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
	if got := plan.Topology.Sources["office"].StatePath; got != "" {
		t.Fatalf("topology office StatePath = %q, want empty declarative value", got)
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
	if got := plan.Topology.Sources["vault"].StatePath; got != "" {
		t.Fatalf("topology vault StatePath = %q, want empty declarative value", got)
	}
	if got := plan.Topology.Routes["archive"].StateDir; got != "" {
		t.Fatalf("topology route StateDir = %q, want empty declarative value", got)
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

	_, err := ResolveRoutePlan(testRuntimeConfig(stateDir, topologyPath), Selector{RouteName: "archive"}, RoutePlanSingleSource)
	if err == nil || !strings.Contains(err.Error(), "显式指定 --source") {
		t.Fatalf("ResolveRoutePlan() error = %v, want explicit source selection", err)
	}
}

func TestResolveSingleSourceRunUsesRouteSelection(t *testing.T) {
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
				Folder:       "Inbox/Sub",
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
				SourceRefs: []string{"office"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
	}
	writeTopologyFile(t, topologyPath, topology)

	run, err := ResolveSingleSourceRun(testRuntimeConfig(stateDir, topologyPath), Selector{})
	if err != nil {
		t.Fatalf("ResolveSingleSourceRun() error = %v", err)
	}
	if run.Source.Name != "office" || run.Route.Name != "archive" {
		t.Fatalf("unexpected run selection: %+v", run)
	}
}

func TestResolveSingleSourceRunDoesNotInjectBackupTargetFromPipelineConfig(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	topology := appconfig.Topology{
		Sources: map[string]appconfig.Source{
			"office": {
				Name:         "office",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "Inbox/Sub",
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
				SourceRefs: []string{"office"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
		DefaultRoute:  "archive",
		DefaultSource: "office",
	}
	writeTopologyFile(t, topologyPath, topology)

	cfg := testRuntimeConfig(stateDir, topologyPath)
	cfg.Mail.Pipeline.BackupDir = filepath.Join(stateDir, "backup")

	run, err := ResolveSingleSourceRun(cfg, Selector{})
	if err != nil {
		t.Fatalf("ResolveSingleSourceRun() error = %v", err)
	}
	if len(run.Route.Targets) != 1 {
		t.Fatalf("len(Route.Targets) = %d, want 1 declarative target", len(run.Route.Targets))
	}
	if _, ok := run.Sinks["backup"]; ok {
		t.Fatalf("unexpected implicit backup sink in run.Sinks")
	}
	if len(run.RuntimeTargets) != 1 {
		t.Fatalf("len(RuntimeTargets) = %d, want 1", len(run.RuntimeTargets))
	}
	if got := run.RuntimeTargets[0].SinkRef; got != "discard" {
		t.Fatalf("RuntimeTargets[0].SinkRef = %q, want discard", got)
	}
	if len(run.ExecutionPlan.Targets) != 1 {
		t.Fatalf("len(ExecutionPlan.Targets) = %d, want 1", len(run.ExecutionPlan.Targets))
	}
}

func TestResolveSingleSourceRunIncludesExplicitBackupSink(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	backupDir := filepath.Join(stateDir, "topology-backup")
	topology := appconfig.Topology{
		Sources: map[string]appconfig.Source{
			"office": {
				Name:         "office",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "Inbox/Sub",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"discard": {Name: "discard", Driver: "discard"},
			"backup": {
				Name:      "backup",
				Driver:    "backup",
				OutputDir: backupDir,
			},
		},
		Routes: map[string]appconfig.Route{
			"archive": {
				Name:       "archive",
				SourceRefs: []string{"office"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
					{Name: "backup", SinkRef: "backup", Artifact: "backup", Required: false},
				},
			},
		},
		DefaultRoute:  "archive",
		DefaultSource: "office",
	}
	writeTopologyFile(t, topologyPath, topology)

	cfg := testRuntimeConfig(stateDir, topologyPath)
	cfg.Mail.Pipeline.BackupDir = filepath.Join(stateDir, "env-backup")

	run, err := ResolveSingleSourceRun(cfg, Selector{})
	if err != nil {
		t.Fatalf("ResolveSingleSourceRun() error = %v", err)
	}
	if len(run.RuntimeTargets) != 2 {
		t.Fatalf("len(RuntimeTargets) = %d, want 2", len(run.RuntimeTargets))
	}
	sink, ok := run.Sinks["backup"]
	if !ok {
		t.Fatalf("missing explicit backup sink in run.Sinks")
	}
	if sink.Sink.OutputDir != backupDir {
		t.Fatalf("backup sink OutputDir = %q, want %q", sink.Sink.OutputDir, backupDir)
	}
	if sink.Config.Auth.StateDir != stateDir {
		t.Fatalf("backup sink Auth.StateDir = %q, want %q", sink.Config.Auth.StateDir, stateDir)
	}
	if len(run.ExecutionPlan.Targets) != 2 {
		t.Fatalf("len(ExecutionPlan.Targets) = %d, want 2", len(run.ExecutionPlan.Targets))
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
	if got := plan.Topology.Sources["archive"].StatePath; got != "" {
		t.Fatalf("topology Source.StatePath = %q, want empty declarative value", got)
	}
}

func TestResolveRoutePlanFallsBackSinkMailboxToSourceFolder(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	topology := appconfig.Topology{
		Credentials: map[string]appconfig.Credential{
			"default": {Name: "default", Kind: "shared-session"},
		},
		Sources: map[string]appconfig.Source{
			"office": {
				Name:         "office",
				Driver:       "imap",
				Mode:         "poll",
				Folder:       "Inbox/Archive",
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"archive": {
				Name:   "archive",
				Driver: "imap",
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

	plan, err := ResolveRoutePlan(testRuntimeConfig(stateDir, topologyPath), Selector{}, RoutePlanSingleSource)
	if err != nil {
		t.Fatalf("ResolveRoutePlan() error = %v", err)
	}

	sink, ok := plan.Runs[0].Sinks["archive"]
	if !ok {
		t.Fatalf("missing archive sink")
	}
	if got, want := sink.Mailbox, "Inbox/Archive"; got != want {
		t.Fatalf("Mailbox = %q, want %q", got, want)
	}
	if sink.Sink.Folder != "" {
		t.Fatalf("configured sink folder = %q, want empty declarative value", sink.Sink.Folder)
	}
	if got := plan.Topology.Sinks["archive"].Folder; got != "" {
		t.Fatalf("topology sink folder = %q, want empty declarative value", got)
	}
}

func TestResolveRoutePlanLeavesPushSourceStatePathEmpty(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	topology := appconfig.Topology{
		Sources: map[string]appconfig.Source{
			"incoming": mustWebhookSource(t, appconfig.Source{
				Name:   "incoming",
				Driver: "webhook",
				Mode:   "push",
			}),
		},
		Sinks: map[string]appconfig.Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]appconfig.Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"incoming"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Artifact: "primary", Required: true},
				},
			},
		},
		DefaultRoute:  "default",
		DefaultSource: "incoming",
	}
	writeTopologyFile(t, topologyPath, topology)

	plan, err := ResolveRoutePlan(testRuntimeConfig(stateDir, topologyPath), Selector{}, RoutePlanSingleSource)
	if err != nil {
		t.Fatalf("ResolveRoutePlan() error = %v", err)
	}
	if len(plan.Runs) != 1 {
		t.Fatalf("len(Runs) = %d, want 1", len(plan.Runs))
	}
	if got := plan.Runs[0].Source.StatePath; got != "" {
		t.Fatalf("push source StatePath = %q, want empty", got)
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

	plan, err := ResolveRoutePlan(testRuntimeConfig(stateDir, topologyPath), Selector{}, RoutePlanSingleSource)
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

func TestResolveRoutePlanAllowsLocalSinkWithoutCredentialResolution(t *testing.T) {
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
				Name:          "archive",
				Driver:        "imap",
				Mode:          "poll",
				CredentialRef: "office-auth",
				Folder:        "Archive/2026",
				PollInterval:  time.Minute,
				CycleTimeout:  2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"local-output": {
				Name:      "local-output",
				Driver:    "file",
				OutputDir: filepath.Join(stateDir, "output"),
			},
		},
		Routes: map[string]appconfig.Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"archive"},
				Targets: []appconfig.RouteTarget{
					{Name: "local-output", SinkRef: "local-output", Artifact: "primary", Required: true},
				},
			},
		},
	}
	writeTopologyFile(t, topologyPath, topology)

	plan, err := ResolveRoutePlan(testRuntimeConfig(stateDir, topologyPath), Selector{}, RoutePlanAllSources)
	if err != nil {
		t.Fatalf("ResolveRoutePlan() error = %v", err)
	}
	sink, ok := plan.Runs[0].Sinks["local-output"]
	if !ok {
		t.Fatalf("missing local-output sink")
	}
	if sink.Config.Auth.StateDir != stateDir {
		t.Fatalf("sink Auth.StateDir = %q, want %q", sink.Config.Auth.StateDir, stateDir)
	}
}

func TestResolveRoutePlanDoesNotPopulateStatePathForPushWebhookSource(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	topology := appconfig.Topology{
		DefaultRoute: "default",
		Sources: map[string]appconfig.Source{
			"incoming": mustWebhookSource(t, appconfig.Source{
				Name:   "incoming",
				Driver: "webhook",
				Mode:   "push",
			}),
		},
		Sinks: map[string]appconfig.Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]appconfig.Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"incoming"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Required: true},
				},
			},
		},
	}
	writeTopologyFile(t, topologyPath, topology)

	plan, err := ResolveRoutePlan(testRuntimeConfig(stateDir, topologyPath), Selector{}, RoutePlanSingleSource)
	if err != nil {
		t.Fatalf("ResolveRoutePlan() error = %v", err)
	}
	if len(plan.Runs) != 1 {
		t.Fatalf("len(Runs) = %d, want 1", len(plan.Runs))
	}

	run := plan.Runs[0]
	if run.Source.StatePath != "" {
		t.Fatalf("Source.StatePath = %q, want empty", run.Source.StatePath)
	}
	if got, want := run.Route.StateDir, filepath.Join(stateDir, "flow-state", "default-incoming-webhook"); got != want {
		t.Fatalf("Route.StateDir = %q, want %q", got, want)
	}
	if got := plan.Topology.Sources["incoming"].StatePath; got != "" {
		t.Fatalf("topology Source.StatePath = %q, want empty declarative value", got)
	}
	if got := plan.Topology.Routes["default"].StateDir; got != "" {
		t.Fatalf("topology Route.StateDir = %q, want empty declarative value", got)
	}
}

func TestResolveRoutePlanRejectsUnknownNestedWebhookConfigField(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	content := `{
  "sources": {
    "incoming": {
      "name": "incoming",
      "driver": "webhook",
      "mode": "push",
      "config": {
        "listen_addr": "127.0.0.1:8080",
        "path": "/mail/incoming",
        "secret_env": "MIMECRYPT_WEBHOOK_SECRET",
        "unexpected": true
      }
    }
  },
  "sinks": {
    "discard": {
      "name": "discard",
      "driver": "discard"
    }
  },
  "routes": {
    "default": {
      "name": "default",
      "source_refs": ["incoming"],
      "targets": [
        {"name": "discard", "sink_ref": "discard", "required": true}
      ]
    }
  },
  "default_source": "incoming",
  "default_route": "default"
}`
	if err := os.WriteFile(topologyPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ResolveRoutePlan(testRuntimeConfig(stateDir, topologyPath), Selector{}, RoutePlanSingleSource)
	if err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("ResolveRoutePlan() error = %v, want nested config unknown field rejection", err)
	}
}

func testRuntimeConfig(stateDir, topologyPath string) appconfig.Config {
	return appconfig.Config{
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
				GraphBaseURL: "https://graph.microsoft.com/v1.0",
				EWSBaseURL:   "https://outlook.office365.com/EWS/Exchange.asmx",
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
