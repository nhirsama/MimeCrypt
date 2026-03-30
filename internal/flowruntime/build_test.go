package flowruntime

import (
	"context"
	"path/filepath"
	"testing"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow/adapters"
	"mimecrypt/internal/provider"
)

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

func TestBuildLocalConsumerUsesCapabilityKind(t *testing.T) {
	t.Parallel()

	consumer, err := buildLocalConsumer(SinkPlan{
		Sink: appconfig.Sink{
			Name:      "local-output",
			Driver:    "file",
			OutputDir: "/tmp/out",
		},
	}, &provider.SinkSpec{
		LocalConsumer:     true,
		LocalConsumerKind: provider.LocalConsumerFile,
	})
	if err != nil {
		t.Fatalf("buildLocalConsumer() error = %v", err)
	}
	fileConsumer, ok := consumer.(*adapters.FileConsumer)
	if !ok {
		t.Fatalf("consumer type = %T, want *adapters.FileConsumer", consumer)
	}
	if fileConsumer.OutputDir != "/tmp/out" {
		t.Fatalf("OutputDir = %q, want /tmp/out", fileConsumer.OutputDir)
	}
}
