package flowruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow/adapters"
)

func TestBuildPushRuntimeBuildsWebhookIngressAndProducer(t *testing.T) {
	t.Setenv("MIMECRYPT_WEBHOOK_SECRET", "top-secret")

	run := SourceRun{
		Source: appconfig.Source{
			Name:   "incoming",
			Driver: "webhook",
			Mode:   "push",
			Webhook: &appconfig.WebhookSource{
				ListenAddr:         "127.0.0.1:0",
				Path:               "/mail/incoming",
				SecretEnv:          "MIMECRYPT_WEBHOOK_SECRET",
				TimestampTolerance: time.Minute,
			},
		},
		Route: appconfig.Route{
			Name:     "default",
			StateDir: t.TempDir(),
			Targets: []appconfig.RouteTarget{
				{Name: "discard", SinkRef: "discard", Required: true},
			},
		},
		Config: appconfig.Config{
			Auth: appconfig.AuthConfig{StateDir: t.TempDir()},
			Mail: appconfig.MailConfig{
				Pipeline: appconfig.MailPipelineConfig{
					AuditLogPath: filepath.Join(t.TempDir(), "audit.jsonl"),
				},
			},
		},
		Sinks: map[string]SinkPlan{
			"discard": {Sink: appconfig.Sink{Name: "discard", Driver: "discard"}},
		},
	}

	runtime, err := BuildPushRuntime(context.Background(), run)
	if err != nil {
		t.Fatalf("BuildPushRuntime() error = %v", err)
	}
	if runtime == nil || runtime.Runner == nil || runtime.Ingress == nil {
		t.Fatalf("BuildPushRuntime() returned incomplete runtime: %+v", runtime)
	}

	ingress, ok := runtime.Ingress.(*webhookIngress)
	if !ok {
		t.Fatalf("Ingress type = %T, want *webhookIngress", runtime.Ingress)
	}
	if ingress.path != "/mail/incoming" {
		t.Fatalf("ingress path = %q, want /mail/incoming", ingress.path)
	}

	producer, ok := runtime.Runner.Producer.(*adapters.PushProducer)
	if !ok {
		t.Fatalf("Producer type = %T, want *adapters.PushProducer", runtime.Runner.Producer)
	}
	if producer.Spool == nil {
		t.Fatalf("PushProducer.Spool = nil")
	}
	if got, want := producer.Spool.Dir, filepath.Join(run.Route.StateDir, "push-spool"); got != want {
		t.Fatalf("PushProducer.Spool.Dir = %q, want %q", got, want)
	}
	if got, want := producer.Store.Driver, "webhook"; got != want {
		t.Fatalf("producer store driver = %q, want %q", got, want)
	}
	if got, want := producer.Store.Account, "incoming"; got != want {
		t.Fatalf("producer store account = %q, want %q", got, want)
	}
}

func TestBuildPushRuntimeRejectsNonPushMode(t *testing.T) {
	t.Parallel()

	_, err := BuildPushRuntime(context.Background(), SourceRun{
		Source: appconfig.Source{
			Name:   "incoming",
			Driver: "webhook",
			Mode:   "poll",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "mode=push") {
		t.Fatalf("BuildPushRuntime() error = %v, want push mode rejection", err)
	}
}
