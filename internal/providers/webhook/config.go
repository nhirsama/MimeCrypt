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

	webhook := source.Webhook
	if webhook == nil {
		webhook = &appconfig.WebhookSource{}
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

	source.Driver = "webhook"
	source.Mode = "push"
	source.Webhook = &appconfig.WebhookSource{
		ListenAddr:         listenAddr,
		Path:               path,
		SecretEnv:          secretEnv,
		MaxBodyBytes:       maxBodyBytes,
		TimestampTolerance: timestampTolerance,
	}
	return source, nil
}

func DescribeSource(source appconfig.Source) []string {
	lines := []string{
		fmt.Sprintf("source=%s driver=webhook mode=push", strings.TrimSpace(source.Name)),
	}
	if source.Webhook == nil {
		return lines
	}
	lines = append(lines,
		fmt.Sprintf("listen_addr=%s", strings.TrimSpace(source.Webhook.ListenAddr)),
		fmt.Sprintf("path=%s", strings.TrimSpace(source.Webhook.Path)),
		fmt.Sprintf("secret_env=%s", strings.TrimSpace(source.Webhook.SecretEnv)),
		fmt.Sprintf("max_body_bytes=%d", source.Webhook.MaxBodyBytes),
		fmt.Sprintf("timestamp_tolerance=%s", source.Webhook.TimestampTolerance),
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
