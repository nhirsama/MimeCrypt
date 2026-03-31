package webhook

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/interact"
)

const (
	defaultConfigListenAddr = "127.0.0.1:8080"
	defaultConfigPath       = "/mail/incoming"
	defaultConfigSecretEnv  = "MIMECRYPT_WEBHOOK_SECRET"
)

type SourceConfig struct {
	ListenAddr         string        `json:"listen_addr,omitempty"`
	Path               string        `json:"path,omitempty"`
	SecretEnv          string        `json:"secret_env,omitempty"`
	MaxBodyBytes       int64         `json:"max_body_bytes,omitempty"`
	TimestampTolerance time.Duration `json:"timestamp_tolerance,omitempty"`
}

func DecodeSourceConfig(source appconfig.Source) (SourceConfig, error) {
	var cfg SourceConfig
	if err := source.DecodeDriverConfig(&cfg); err != nil {
		return SourceConfig{}, err
	}
	return cfg, nil
}

func WithSourceConfig(source appconfig.Source, cfg SourceConfig) (appconfig.Source, error) {
	source.Driver = "webhook"
	source.Mode = "push"
	return source.WithDriverConfig(cfg)
}

func ConfigureSource(source appconfig.Source, in io.Reader, out io.Writer) (appconfig.Source, error) {
	if in == nil {
		in = strings.NewReader("")
	}
	if out == nil {
		out = io.Discard
	}
	reader, ok := in.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(in)
	}

	webhook := SourceConfig{}
	if len(source.DriverConfig) > 0 {
		var err error
		webhook, err = DecodeSourceConfig(source)
		if err != nil {
			return appconfig.Source{}, err
		}
	}

	_, _ = fmt.Fprintln(out, "配置 webhook source（输入 q 可退出）")

	listenAddr, err := interact.PromptString(reader, out, "Listen Addr", firstNonEmptyString(strings.TrimSpace(webhook.ListenAddr), defaultConfigListenAddr), false)
	if err != nil {
		return appconfig.Source{}, err
	}
	path, err := interact.PromptString(reader, out, "Path", firstNonEmptyString(strings.TrimSpace(webhook.Path), defaultConfigPath), false)
	if err != nil {
		return appconfig.Source{}, err
	}
	secretEnv, err := interact.PromptString(reader, out, "Secret Env", firstNonEmptyString(strings.TrimSpace(webhook.SecretEnv), defaultConfigSecretEnv), false)
	if err != nil {
		return appconfig.Source{}, err
	}
	maxBodyBytes, err := interact.PromptInt64(reader, out, "Max Body Bytes", firstNonEmptyInt64(webhook.MaxBodyBytes, defaultMaxBodyBytes))
	if err != nil {
		return appconfig.Source{}, err
	}
	timestampTolerance, err := interact.PromptDuration(reader, out, "Timestamp Tolerance", firstNonEmptyDuration(webhook.TimestampTolerance, defaultTimestampTolerance))
	if err != nil {
		return appconfig.Source{}, err
	}

	return WithSourceConfig(source, SourceConfig{
		ListenAddr:         listenAddr,
		Path:               path,
		SecretEnv:          secretEnv,
		MaxBodyBytes:       maxBodyBytes,
		TimestampTolerance: timestampTolerance,
	})
}

func DescribeSource(source appconfig.Source) []string {
	lines := []string{
		fmt.Sprintf("source=%s driver=webhook mode=push", strings.TrimSpace(source.Name)),
	}
	webhook, err := DecodeSourceConfig(source)
	if err != nil {
		return lines
	}
	lines = append(lines,
		fmt.Sprintf("listen_addr=%s", strings.TrimSpace(webhook.ListenAddr)),
		fmt.Sprintf("path=%s", strings.TrimSpace(webhook.Path)),
		fmt.Sprintf("secret_env=%s", strings.TrimSpace(webhook.SecretEnv)),
		fmt.Sprintf("max_body_bytes=%d", webhook.MaxBodyBytes),
		fmt.Sprintf("timestamp_tolerance=%s", webhook.TimestampTolerance),
	)
	return lines
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmptyDuration(values ...time.Duration) time.Duration {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
