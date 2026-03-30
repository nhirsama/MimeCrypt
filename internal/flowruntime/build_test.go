package flowruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow/adapters"
	"mimecrypt/internal/providers"
)

func TestBuildMailflowPlanAddsConfiguredTargets(t *testing.T) {
	t.Parallel()

	route := appconfig.Route{
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
	}
	plan, err := buildMailflowPlan(route, route.Targets)
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

	route := appconfig.Route{
		Name: "default",
		Targets: []appconfig.RouteTarget{
			{Name: "discard-primary", SinkRef: "discard", Artifact: "primary", Required: true},
		},
	}
	plan, err := buildMailflowPlan(route, route.Targets)
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

func TestBuildMailflowSinkStoreUsesExplicitMailbox(t *testing.T) {
	t.Parallel()

	store, err := buildMailflowSinkStore(context.Background(), appconfig.Config{
		Mail: appconfig.MailConfig{},
	}, nil, "imap", "archive-2026", false)
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
	if got.Mail.Sync.StateDir != filepath.Join("/state", "credentials", "archive-auth") {
		t.Fatalf("Mail.Sync.StateDir = %q", got.Mail.Sync.StateDir)
	}
	if got.Auth.TokenStore != "keyring" {
		t.Fatalf("TokenStore = %q, want keyring", got.Auth.TokenStore)
	}
}

func TestBuildLocalConsumerUsesRegistryCapabilityKind(t *testing.T) {
	t.Parallel()

	consumer, err := providers.BuildLocalConsumer(appconfig.Config{}, appconfig.Sink{
		Name:      "local-output",
		Driver:    "file",
		OutputDir: "/tmp/out",
	}, nil)
	if err != nil {
		t.Fatalf("BuildLocalConsumer() error = %v", err)
	}
	fileConsumer, ok := consumer.(*adapters.FileConsumer)
	if !ok {
		t.Fatalf("consumer type = %T, want *adapters.FileConsumer", consumer)
	}
	if fileConsumer.OutputDir != "/tmp/out" {
		t.Fatalf("OutputDir = %q, want /tmp/out", fileConsumer.OutputDir)
	}
}

func TestBuildLocalConsumerUsesBackupCapabilityKind(t *testing.T) {
	t.Parallel()

	consumer, err := providers.BuildLocalConsumer(appconfig.Config{}, appconfig.Sink{
		Name:      "backup",
		Driver:    "backup",
		OutputDir: "/tmp/backup",
	}, nil)
	if err != nil {
		t.Fatalf("BuildLocalConsumer() error = %v", err)
	}
	backupConsumer, ok := consumer.(*adapters.BackupConsumer)
	if !ok {
		t.Fatalf("consumer type = %T, want *adapters.BackupConsumer", consumer)
	}
	if backupConsumer.OutputDir != "/tmp/backup" {
		t.Fatalf("OutputDir = %q, want /tmp/backup", backupConsumer.OutputDir)
	}
}

func TestBuildHealthServiceAllowsWebhookSourceWithoutProviderClients(t *testing.T) {
	t.Parallel()

	run := SourceRun{
		Source: appconfig.Source{
			Name:   "incoming",
			Driver: "webhook",
			Mode:   "push",
			Webhook: &appconfig.WebhookSource{
				ListenAddr: "127.0.0.1:8080",
				Path:       "/mail/incoming",
				SecretEnv:  "MIMECRYPT_WEBHOOK_SECRET",
			},
		},
		Route: appconfig.Route{
			Name: "default",
			Targets: []appconfig.RouteTarget{
				{Name: "discard", SinkRef: "discard", Required: true},
			},
		},
		Config: appconfig.Config{
			Auth: appconfig.AuthConfig{StateDir: t.TempDir()},
		},
		Sinks: map[string]SinkPlan{
			"discard": {Sink: appconfig.Sink{Name: "discard", Driver: "discard"}},
		},
	}

	service, err := BuildHealthService(context.Background(), run)
	if err != nil {
		t.Fatalf("BuildHealthService() error = %v", err)
	}
	if !service.SkipCachedToken || !service.SkipProviderProbe {
		t.Fatalf("unexpected health service flags: %+v", service)
	}
	if service.Session != nil || service.Reader != nil {
		t.Fatalf("webhook health should not require session/reader: %+v", service)
	}
}

func TestBuildRunnerRejectsPushModeSource(t *testing.T) {
	t.Parallel()

	_, err := BuildRunner(context.Background(), SourceRun{
		Source: appconfig.Source{
			Name:   "incoming",
			Driver: "webhook",
			Mode:   "push",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "mode=push") {
		t.Fatalf("BuildRunner() error = %v, want push mode rejection", err)
	}
}
