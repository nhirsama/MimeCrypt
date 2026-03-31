package appconfig

import (
	"testing"
	"time"
)

type webhookSourceConfig struct {
	ListenAddr         string        `json:"listen_addr,omitempty"`
	Path               string        `json:"path,omitempty"`
	SecretEnv          string        `json:"secret_env,omitempty"`
	MaxBodyBytes       int64         `json:"max_body_bytes,omitempty"`
	TimestampTolerance time.Duration `json:"timestamp_tolerance,omitempty"`
}

func mustWebhookSource(t testing.TB, source Source) Source {
	t.Helper()

	config, err := source.WithDriverConfig(webhookSourceConfig{
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

func mustWebhookSourceWithConfig(t testing.TB, source Source, cfg webhookSourceConfig) Source {
	t.Helper()

	config, err := source.WithDriverConfig(cfg)
	if err != nil {
		t.Fatalf("WithSourceConfig() error = %v", err)
	}
	config.Driver = "webhook"
	config.Mode = "push"
	return config
}
