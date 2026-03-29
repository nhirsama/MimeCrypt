package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
)

func TestValidateMailflowFlagsRejectsDeleteSourceWithoutWriteBack(t *testing.T) {
	t.Parallel()

	err := validateMailflowFlags(true, false, false, true, "")
	if err == nil || !strings.Contains(err.Error(), "--delete-source 依赖 --write-back") {
		t.Fatalf("validateMailflowFlags() error = %v, want delete-source validation", err)
	}
}

func TestRunCommandExposesDeleteSourceFlag(t *testing.T) {
	t.Parallel()

	cmd := newRunCmd()
	if cmd.Flags().Lookup("delete-source") == nil {
		t.Fatalf("expected delete-source flag on run command")
	}
}

func TestRunCommandExposesTopologyFlags(t *testing.T) {
	t.Parallel()

	cmd := newRunCmd()
	for _, name := range []string{"topology-file", "source", "route"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected %s flag on run command", name)
		}
	}
}

func TestDownloadCommandExposesSourceTopologyFlagsOnly(t *testing.T) {
	t.Parallel()

	cmd := newDownloadCmd()
	for _, name := range []string{"topology-file", "source"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected %s flag on download command", name)
		}
	}
	if cmd.Flags().Lookup("route") != nil {
		t.Fatalf("did not expect route flag on download command")
	}
}

func TestListCommandExposesSourceTopologyFlagsOnly(t *testing.T) {
	t.Parallel()

	cmd := newListCmd()
	for _, name := range []string{"topology-file", "source"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected %s flag on list command", name)
		}
	}
	if cmd.Flags().Lookup("route") != nil {
		t.Fatalf("did not expect route flag on list command")
	}
}

func TestHealthCommandExposesTopologyFlags(t *testing.T) {
	t.Parallel()

	cmd := newHealthCmd()
	for _, name := range []string{"topology-file", "source", "route"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected %s flag on health command", name)
		}
	}
}

func TestLoginCommandExposesCredentialFlags(t *testing.T) {
	t.Parallel()

	cmd := newLoginCmd()
	for _, name := range []string{"topology-file", "credential"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected %s flag on login command", name)
		}
	}
}

func TestTokenStatusCommandExposesCredentialFlags(t *testing.T) {
	t.Parallel()

	cmd := newTokenStatusCmd(appconfig.Config{})
	for _, name := range []string{"topology-file", "credential"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected %s flag on token status command", name)
		}
	}
}

func TestLogoutCommandExposesCredentialFlags(t *testing.T) {
	t.Parallel()

	cmd := newLogoutCmd()
	for _, name := range []string{"topology-file", "credential"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("expected %s flag on logout command", name)
		}
	}
}

func TestFlowRunCommandIsHiddenDeprecatedAlias(t *testing.T) {
	t.Parallel()

	cmd := newFlowRunCmd()
	if !cmd.Hidden {
		t.Fatalf("Hidden = false, want true")
	}
	if cmd.Deprecated == "" || !strings.Contains(cmd.Deprecated, "请改用 run") {
		t.Fatalf("Deprecated = %q, want migration hint", cmd.Deprecated)
	}
}

func TestBuildMailflowPlanAddsConfiguredTargets(t *testing.T) {
	t.Parallel()

	plan, err := buildMailflowPlan(appconfig.Route{
		Name: "default",
		Targets: []appconfig.RouteTarget{
			{Name: "local-output", SinkRef: "local-output", Artifact: "primary", Required: true},
			{Name: "write-back", SinkRef: "write-back", Artifact: "primary", Required: true},
		},
		DeleteSource: appconfig.DeleteSourcePolicy{
			Enabled:          true,
			RequireSameStore: true,
			EligibleSinks:    []string{"write-back"},
		},
	})
	if err != nil {
		t.Fatalf("buildMailflowPlan() error = %v", err)
	}
	if len(plan.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(plan.Targets))
	}
	if !plan.DeleteSource.Enabled {
		t.Fatalf("DeleteSource.Enabled = false, want true")
	}
	if got := plan.DeleteSource.EligibleConsumers; len(got) != 1 || got[0] != "write-back" {
		t.Fatalf("EligibleConsumers = %+v, want [write-back]", got)
	}
}

func TestBuildMailflowPlanFallsBackToDiscardTarget(t *testing.T) {
	t.Parallel()

	plan, err := buildMailflowPlan(appconfig.Route{
		Name: "default",
		Targets: []appconfig.RouteTarget{
			{Name: "discard-primary", SinkRef: "discard", Artifact: "primary", Required: true},
		},
	})
	if err != nil {
		t.Fatalf("buildMailflowPlan() error = %v", err)
	}
	if len(plan.Targets) != 1 {
		t.Fatalf("len(Targets) = %d, want 1", len(plan.Targets))
	}
	if plan.Targets[0].Consumer != "discard" {
		t.Fatalf("Consumer = %q, want discard", plan.Targets[0].Consumer)
	}
}

func TestSummarizeMailflowResultUsesTraceAndDeliveries(t *testing.T) {
	t.Parallel()

	outputPath := filepath.Join(t.TempDir(), "out.eml")
	if err := os.WriteFile(outputPath, []byte("encrypted"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	summary, err := summarizeMailflowResult(mailflow.Result{
		Key: "tx-summary",
		Trace: mailflow.MailTrace{
			SourceMessageID: "m1",
			Attributes: map[string]string{
				"format":      "pgp-mime",
				"backup_path": "/backup/m1.pgp",
			},
		},
		Deliveries: map[string]mailflow.DeliveryReceipt{
			"local-output": {
				Consumer: "local-output",
				ID:       outputPath,
			},
			"write-back": {
				Consumer: "write-back",
				Verified: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("summarizeMailflowResult() error = %v", err)
	}
	if !summary.Encrypted || summary.AlreadyEncrypted {
		t.Fatalf("unexpected summary flags: %+v", summary)
	}
	if !summary.SavedOutput || summary.Path != outputPath {
		t.Fatalf("unexpected output summary: %+v", summary)
	}
	if !summary.WroteBack || !summary.Verified {
		t.Fatalf("unexpected write-back summary: %+v", summary)
	}
	if summary.BackupPath != "/backup/m1.pgp" || summary.Format != "pgp-mime" {
		t.Fatalf("unexpected trace summary: %+v", summary)
	}
}

func TestSummarizeMailflowResultMarksAlreadyEncryptedSkip(t *testing.T) {
	t.Parallel()

	summary, err := summarizeMailflowResult(mailflow.Result{
		Key:     "tx-skip",
		Skipped: true,
		Trace: mailflow.MailTrace{
			SourceMessageID: "m2",
			Attributes: map[string]string{
				"already_encrypted": "true",
				"format":            "pgp-mime",
			},
		},
	})
	if err != nil {
		t.Fatalf("summarizeMailflowResult() error = %v", err)
	}
	if !summary.AlreadyEncrypted || summary.Encrypted {
		t.Fatalf("unexpected summary flags: %+v", summary)
	}
}

func TestRunMailflowCycleSeparatesSkippedMessages(t *testing.T) {
	t.Parallel()

	runner := &fakeCycleRunner{
		results: []fakeCycleResult{
			{result: mailflow.Result{Skipped: false}, processed: true},
			{result: mailflow.Result{Skipped: true}, processed: true},
			{result: mailflow.Result{}, processed: false},
		},
	}

	processed, skipped, deleted, err := runMailflowCycle(context.Background(), time.Minute, runner)
	if err != nil {
		t.Fatalf("runMailflowCycle() error = %v", err)
	}
	if processed != 1 || skipped != 1 || deleted != 0 {
		t.Fatalf("unexpected counts: processed=%d skipped=%d deleted=%d", processed, skipped, deleted)
	}
}

func TestResolveMailflowTopologyLoadsConfiguredSourceAndRoute(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	content, err := json.Marshal(appconfig.Topology{
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
			"archive": {
				Name:      "archive",
				Driver:    "file",
				OutputDir: filepath.Join(stateDir, "output"),
			},
		},
		Routes: map[string]appconfig.Route{
			"archive": {
				Name:       "archive",
				SourceRefs: []string{"office"},
				Targets: []appconfig.RouteTarget{
					{Name: "archive", SinkRef: "archive", Artifact: "primary", Required: true},
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

	resolved, err := resolveMailflowTopology(appconfig.Config{
		TopologyPath: topologyPath,
		Auth:         appconfig.AuthConfig{StateDir: stateDir},
		Mail: appconfig.MailConfig{
			Sync: appconfig.MailSyncConfig{
				StateDir: stateDir,
			},
		},
	}, topologyConfigFlags{topologyFile: topologyPath}, appconfig.TopologyOptions{})
	if err != nil {
		t.Fatalf("resolveMailflowTopology() error = %v", err)
	}
	if !resolved.Custom {
		t.Fatalf("Custom = false, want true")
	}
	if resolved.Source.Name != "office" || resolved.Route.Name != "archive" {
		t.Fatalf("unexpected resolved topology: %+v", resolved)
	}
	if resolved.Source.StatePath != filepath.Join(stateDir, "flow-sync-office-imap-Inbox_Sub.json") {
		t.Fatalf("unexpected source state path: %q", resolved.Source.StatePath)
	}
	if resolved.Route.StateDir != filepath.Join(stateDir, "flow-state", "archive-office-imap-Inbox_Sub") {
		t.Fatalf("unexpected route state dir: %q", resolved.Route.StateDir)
	}
}

func TestResolveMailflowTopologyRejectsNonDefaultSelectionInLegacyMode(t *testing.T) {
	t.Parallel()

	_, err := resolveMailflowTopology(appconfig.Config{
		Provider: "imap",
		Auth:     appconfig.AuthConfig{StateDir: t.TempDir()},
		Mail: appconfig.MailConfig{
			Pipeline: appconfig.MailPipelineConfig{
				BackupDir:    "backup",
				AuditLogPath: "audit.jsonl",
			},
			Sync: appconfig.MailSyncConfig{
				Folder:       "INBOX",
				StateDir:     t.TempDir(),
				PollInterval: time.Minute,
				CycleTimeout: 2 * time.Minute,
			},
		},
	}, topologyConfigFlags{sourceName: "office"}, appconfig.TopologyOptions{})
	if err == nil || !strings.Contains(err.Error(), "legacy 模式只支持 source=default") {
		t.Fatalf("resolveMailflowTopology() error = %v, want legacy selection error", err)
	}
}

func TestResolveTopologySourceRejectsRouteSelection(t *testing.T) {
	t.Parallel()

	_, err := resolveTopologySource(appconfig.Config{}, topologyConfigFlags{routeName: "archive"})
	if err == nil || !strings.Contains(err.Error(), "该命令不支持 route 选择") {
		t.Fatalf("resolveTopologySource() error = %v, want route selection error", err)
	}
}

func TestResolveCredentialConfigLoadsConfiguredCredential(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	content, err := json.Marshal(appconfig.Topology{
		DefaultCredential: "archive-auth",
		DefaultSource:     "default",
		DefaultRoute:      "default",
		Credentials: map[string]appconfig.Credential{
			"archive-auth": {
				Name:           "archive-auth",
				Kind:           "oauth",
				TokenStore:     "keyring",
				KeyringService: "archive-keyring",
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
			"discard": {
				Name:   "discard",
				Driver: "discard",
			},
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

	resolved, err := resolveCredentialConfig(appconfig.Config{
		TopologyPath: topologyPath,
		Auth: appconfig.AuthConfig{
			StateDir:       stateDir,
			TokenStore:     "file",
			KeyringService: "mimecrypt",
		},
	}, credentialConfigFlags{topologyFile: topologyPath})
	if err != nil {
		t.Fatalf("resolveCredentialConfig() error = %v", err)
	}
	if !resolved.Custom || resolved.CredentialName != "archive-auth" {
		t.Fatalf("unexpected resolved credential config: %+v", resolved)
	}
	if got, want := resolved.Config.Auth.StateDir, filepath.Join(stateDir, "credentials", "archive-auth"); got != want {
		t.Fatalf("Auth.StateDir = %q, want %q", got, want)
	}
	if resolved.Config.Auth.TokenStore != "keyring" || resolved.Config.Auth.KeyringService != "archive-keyring" {
		t.Fatalf("unexpected auth config: %+v", resolved.Config.Auth)
	}
}

func TestResolveCredentialConfigRejectsNonDefaultSelectionInLegacyMode(t *testing.T) {
	t.Parallel()

	_, err := resolveCredentialConfig(appconfig.Config{}, credentialConfigFlags{credentialName: "archive-auth"})
	if err == nil || !strings.Contains(err.Error(), "legacy 模式只支持 credential=default") {
		t.Fatalf("resolveCredentialConfig() error = %v, want legacy selection error", err)
	}
}

func TestBuildMailflowSinkStoreFallsBackToSourceFolder(t *testing.T) {
	t.Parallel()

	store, err := buildMailflowSinkStore(context.Background(), appconfig.Config{
		Mail: appconfig.MailConfig{
			Sync: appconfig.MailSyncConfig{Folder: "legacy"},
		},
	}, nil, appconfig.Sink{
		Name:   "archive",
		Driver: "imap",
	}, "archive-2026", false)
	if err != nil {
		t.Fatalf("buildMailflowSinkStore() error = %v", err)
	}
	if got, want := store.Mailbox, "archive-2026"; got != want {
		t.Fatalf("Mailbox = %q, want %q", got, want)
	}
}

func TestApplyTopologyCredentialUsesNamedStateDir(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Auth: appconfig.AuthConfig{
			StateDir:       "/state",
			TokenStore:     "file",
			ClientID:       "client-id",
			Tenant:         "organizations",
			GraphScopes:    []string{"graph.read"},
			EWSScopes:      []string{"ews.read"},
			IMAPScopes:     []string{"imap.read"},
			KeyringService: "mimecrypt",
		},
	}
	topology := appconfig.Topology{
		Credentials: map[string]appconfig.Credential{
			"archive-auth": {
				Name:       "archive-auth",
				Kind:       "oauth",
				TokenStore: "keyring",
			},
		},
	}

	got, err := applyTopologyCredential(cfg, topology, "archive-auth")
	if err != nil {
		t.Fatalf("applyTopologyCredential() error = %v", err)
	}
	if got.Auth.StateDir != filepath.Join("/state", "credentials", "archive-auth") {
		t.Fatalf("Auth.StateDir = %q", got.Auth.StateDir)
	}
	if got.Auth.TokenStore != "keyring" {
		t.Fatalf("TokenStore = %q, want keyring", got.Auth.TokenStore)
	}
}

type fakeCycleRunner struct {
	results []fakeCycleResult
	index   int
}

type fakeCycleResult struct {
	result    mailflow.Result
	processed bool
	err       error
}

func (r *fakeCycleRunner) RunOnce(context.Context) (mailflow.Result, bool, error) {
	if r.index >= len(r.results) {
		return mailflow.Result{}, false, nil
	}
	current := r.results[r.index]
	r.index++
	return current.result, current.processed, current.err
}
