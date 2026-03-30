package flowruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow/adapters"
	"mimecrypt/internal/providers"
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

	producer, ok := runtime.Runner.Producer.(*adapters.PushProducer)
	if !ok {
		t.Fatalf("Producer type = %T, want *adapters.PushProducer", runtime.Runner.Producer)
	}
	if producer.Spool == nil {
		t.Fatalf("PushProducer.Spool = nil")
	}
	if got, want := producer.Spool.Dir, pushSpoolDirForSource(run.Route.StateDir, run.Source); got != want {
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

func TestBuildPushRuntimeUsesSourceScopedSpoolDirWhenStateDirShared(t *testing.T) {
	t.Setenv("MIMECRYPT_WEBHOOK_SECRET", "top-secret")

	sharedStateDir := t.TempDir()
	newRun := func(sourceName string) SourceRun {
		return SourceRun{
			Source: appconfig.Source{
				Name:   sourceName,
				Driver: "webhook",
				Mode:   "push",
				Webhook: &appconfig.WebhookSource{
					ListenAddr: "127.0.0.1:0",
					Path:       "/mail/incoming",
					SecretEnv:  "MIMECRYPT_WEBHOOK_SECRET",
				},
			},
			Route: appconfig.Route{
				Name:     "default",
				StateDir: sharedStateDir,
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Required: true},
				},
			},
			Config: appconfig.Config{
				Mail: appconfig.MailConfig{
					Pipeline: appconfig.MailPipelineConfig{
						AuditLogPath: filepath.Join(t.TempDir(), sourceName+".jsonl"),
					},
				},
			},
			Sinks: map[string]SinkPlan{
				"discard": {Sink: appconfig.Sink{Name: "discard", Driver: "discard"}},
			},
		}
	}

	runtimeA, err := BuildPushRuntime(context.Background(), newRun("incoming-a"))
	if err != nil {
		t.Fatalf("BuildPushRuntime(incoming-a) error = %v", err)
	}
	runtimeB, err := BuildPushRuntime(context.Background(), newRun("incoming-b"))
	if err != nil {
		t.Fatalf("BuildPushRuntime(incoming-b) error = %v", err)
	}

	producerA := runtimeA.Runner.Producer.(*adapters.PushProducer)
	producerB := runtimeB.Runner.Producer.(*adapters.PushProducer)
	if producerA.Spool.Dir == producerB.Spool.Dir {
		t.Fatalf("shared spool dir = %q, want isolated dirs", producerA.Spool.Dir)
	}
}

func TestPushCapableDriversHaveIngressBuilders(t *testing.T) {
	t.Setenv("MIMECRYPT_WEBHOOK_SECRET", "top-secret")

	for _, spec := range providers.AllDriverSpecs() {
		if spec.Source == nil {
			continue
		}
		if _, ok := spec.Source.ModeSpec("push"); !ok {
			continue
		}
		source := appconfig.Source{
			Name:   spec.Name,
			Driver: spec.Name,
			Mode:   "push",
		}
		if spec.Name == "webhook" {
			source.Webhook = &appconfig.WebhookSource{
				ListenAddr: "127.0.0.1:0",
				Path:       "/mail/incoming",
				SecretEnv:  "MIMECRYPT_WEBHOOK_SECRET",
			}
		}
		ingress, err := providers.BuildPushIngress(appconfig.Config{}, appconfig.Route{Name: "default"}, source, &adapters.PushSpool{Dir: t.TempDir()})
		if err != nil {
			t.Fatalf("push-capable driver %s missing ingress builder: %v", spec.Name, err)
		}
		if ingress == nil {
			t.Fatalf("push-capable driver %s returned nil ingress", spec.Name)
		}
	}
}
