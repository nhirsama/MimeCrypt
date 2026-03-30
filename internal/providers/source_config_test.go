package providers

import (
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
)

func TestConfigurableSourceDriversOnlyIncludesInteractiveDrivers(t *testing.T) {
	t.Parallel()

	if got := ConfigurableSourceDrivers(); !reflect.DeepEqual(got, []string{"webhook"}) {
		t.Fatalf("ConfigurableSourceDrivers() = %#v, want [webhook]", got)
	}
}

func TestConfigureSourceConfigWebhookForcesPushMode(t *testing.T) {
	t.Parallel()

	source, err := ConfigureSourceConfig("webhook", appconfig.Source{Name: "incoming", StatePath: "/runtime/flow-sync.json"}, strings.NewReader("\n\n\n2048\n2m\n"), io.Discard)
	if err != nil {
		t.Fatalf("ConfigureSourceConfig() error = %v", err)
	}

	if source.Driver != "webhook" {
		t.Fatalf("Driver = %q, want webhook", source.Driver)
	}
	if source.Mode != "push" {
		t.Fatalf("Mode = %q, want push", source.Mode)
	}
	if source.StatePath != "" {
		t.Fatalf("StatePath = %q, want empty", source.StatePath)
	}
	if source.Webhook == nil {
		t.Fatalf("Webhook = nil")
	}
	if source.Webhook.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("ListenAddr = %q, want 127.0.0.1:8080", source.Webhook.ListenAddr)
	}
	if source.Webhook.Path != "/mail/incoming" {
		t.Fatalf("Path = %q, want /mail/incoming", source.Webhook.Path)
	}
	if source.Webhook.SecretEnv != "MIMECRYPT_WEBHOOK_SECRET" {
		t.Fatalf("SecretEnv = %q, want MIMECRYPT_WEBHOOK_SECRET", source.Webhook.SecretEnv)
	}
	if source.Webhook.MaxBodyBytes != 2048 {
		t.Fatalf("MaxBodyBytes = %d, want 2048", source.Webhook.MaxBodyBytes)
	}
	if source.Webhook.TimestampTolerance != 2*time.Minute {
		t.Fatalf("TimestampTolerance = %s, want 2m", source.Webhook.TimestampTolerance)
	}
}

func TestDescribeSourceConfigUsesDriverSpecificAndFallbackDescriptions(t *testing.T) {
	t.Parallel()

	webhookLines := DescribeSourceConfig(appconfig.Source{
		Name:   "incoming",
		Driver: "webhook",
		Mode:   "push",
		Webhook: &appconfig.WebhookSource{
			ListenAddr:         "127.0.0.1:8080",
			Path:               "/mail/incoming",
			SecretEnv:          "MIMECRYPT_WEBHOOK_SECRET",
			MaxBodyBytes:       2048,
			TimestampTolerance: 2 * time.Minute,
		},
	})
	if len(webhookLines) < 2 {
		t.Fatalf("DescribeSourceConfig(webhook) = %#v, want driver-specific lines", webhookLines)
	}
	if webhookLines[0] != "source=incoming driver=webhook mode=push" {
		t.Fatalf("DescribeSourceConfig(webhook)[0] = %q", webhookLines[0])
	}
	if !reflect.DeepEqual(webhookLines[1:], []string{
		"listen_addr=127.0.0.1:8080",
		"path=/mail/incoming",
		"secret_env=MIMECRYPT_WEBHOOK_SECRET",
		"max_body_bytes=2048",
		"timestamp_tolerance=2m0s",
	}) {
		t.Fatalf("DescribeSourceConfig(webhook) detail = %#v", webhookLines[1:])
	}

	fallbackLines := DescribeSourceConfig(appconfig.Source{
		Name:   "archive",
		Driver: "imap",
		Mode:   "poll",
	})
	if !reflect.DeepEqual(fallbackLines, []string{"source=archive driver=imap mode=poll"}) {
		t.Fatalf("DescribeSourceConfig(imap) = %#v, want generic fallback", fallbackLines)
	}
}

func TestConfigureSourceConfigRejectsNonConfigurableDriver(t *testing.T) {
	t.Parallel()

	_, err := ConfigureSourceConfig("imap", appconfig.Source{Name: "archive", Driver: "imap"}, strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "未提供交互配置") {
		t.Fatalf("ConfigureSourceConfig() error = %v, want interactive config rejection", err)
	}
}
