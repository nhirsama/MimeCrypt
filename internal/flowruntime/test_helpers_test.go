package flowruntime

import (
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
	webhookdevice "mimecrypt/internal/providers/webhook"
)

func mustWebhookSource(t testing.TB, source appconfig.Source) appconfig.Source {
	t.Helper()

	config, err := webhookdevice.WithSourceConfig(source, webhookdevice.SourceConfig{
		ListenAddr:         "127.0.0.1:8080",
		Path:               "/mail/incoming",
		SecretEnv:          "MIMECRYPT_WEBHOOK_SECRET",
		TimestampTolerance: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("WithSourceConfig() error = %v", err)
	}
	return config
}

func mustWebhookSourceWithConfig(t testing.TB, source appconfig.Source, cfg webhookdevice.SourceConfig) appconfig.Source {
	t.Helper()

	config, err := webhookdevice.WithSourceConfig(source, cfg)
	if err != nil {
		t.Fatalf("WithSourceConfig() error = %v", err)
	}
	return config
}
