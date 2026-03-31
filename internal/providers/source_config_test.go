package providers

import (
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
	webhookdevice "mimecrypt/internal/providers/webhook"
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
	webhook, err := webhookdevice.DecodeSourceConfig(source)
	if err != nil {
		t.Fatalf("DecodeSourceConfig() error = %v", err)
	}
	if webhook.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("ListenAddr = %q, want 127.0.0.1:8080", webhook.ListenAddr)
	}
	if webhook.Path != "/mail/incoming" {
		t.Fatalf("Path = %q, want /mail/incoming", webhook.Path)
	}
	if webhook.SecretEnv != "MIMECRYPT_WEBHOOK_SECRET" {
		t.Fatalf("SecretEnv = %q, want MIMECRYPT_WEBHOOK_SECRET", webhook.SecretEnv)
	}
	if webhook.MaxBodyBytes != 2048 {
		t.Fatalf("MaxBodyBytes = %d, want 2048", webhook.MaxBodyBytes)
	}
	if webhook.TimestampTolerance != 2*time.Minute {
		t.Fatalf("TimestampTolerance = %s, want 2m", webhook.TimestampTolerance)
	}
}

func TestDescribeSourceConfigUsesDriverSpecificAndFallbackDescriptions(t *testing.T) {
	t.Parallel()

	source, err := webhookdevice.WithSourceConfig(appconfig.Source{
		Name:   "incoming",
		Driver: "webhook",
		Mode:   "push",
	}, webhookdevice.SourceConfig{
		ListenAddr:         "127.0.0.1:8080",
		Path:               "/mail/incoming",
		SecretEnv:          "MIMECRYPT_WEBHOOK_SECRET",
		MaxBodyBytes:       2048,
		TimestampTolerance: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("WithSourceConfig() error = %v", err)
	}
	webhookLines := DescribeSourceConfig(source)
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
