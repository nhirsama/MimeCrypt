package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
)

func TestConfigWebhookCommandWritesTopology(t *testing.T) {
	t.Setenv("MIMECRYPT_STATE_DIR", t.TempDir())

	topologyPath := filepath.Join(t.TempDir(), "topology.json")
	if err := appconfig.SaveTopologyFile(topologyPath, appconfig.Topology{
		Sinks: map[string]appconfig.Sink{
			"archive": {Name: "archive", Driver: "file", OutputDir: filepath.Join(t.TempDir(), "out")},
		},
	}); err != nil {
		t.Fatalf("SaveTopologyFile() error = %v", err)
	}

	cmd := newConfigSourceCmd()
	cmd.SetArgs([]string{"incoming-webhook", "--topology-file", topologyPath})
	cmd.SetIn(strings.NewReader("\nwebhook\n127.0.0.1:8080\n/mail/incoming\nMIMECRYPT_WEBHOOK_SECRET\n26214400\n5m\n\n\ny\n"))

	if _, err := captureCommandStdout(t, func() error {
		return cmd.Execute()
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	topology, err := appconfig.LoadTopologyFile(topologyPath)
	if err != nil {
		t.Fatalf("LoadTopologyFile() error = %v", err)
	}
	source, ok := topology.Sources["incoming-webhook"]
	if !ok {
		t.Fatalf("webhook source missing: %+v", topology.Sources)
	}
	if source.Driver != "webhook" || source.Mode != "push" {
		t.Fatalf("source = %+v", source)
	}
	if source.Webhook == nil || source.Webhook.Path != "/mail/incoming" || source.Webhook.SecretEnv != "MIMECRYPT_WEBHOOK_SECRET" {
		t.Fatalf("webhook source config = %+v", source.Webhook)
	}
	if source.Webhook.TimestampTolerance != 5*time.Minute {
		t.Fatalf("TimestampTolerance = %s, want 5m", source.Webhook.TimestampTolerance)
	}
	route, ok := topology.Routes["default"]
	if !ok {
		t.Fatalf("route missing: %+v", topology.Routes)
	}
	if len(route.SourceRefs) != 1 || route.SourceRefs[0] != "incoming-webhook" {
		t.Fatalf("route source refs = %#v", route.SourceRefs)
	}
	if len(route.Targets) != 1 || route.Targets[0].SinkRef != "archive" || !route.Targets[0].Required {
		t.Fatalf("route targets = %#v", route.Targets)
	}
	if topology.DefaultSource != "incoming-webhook" || topology.DefaultRoute != "default" {
		t.Fatalf("defaults = source:%q route:%q", topology.DefaultSource, topology.DefaultRoute)
	}
}

func TestConfigSourceCommandAllowsAbort(t *testing.T) {
	t.Setenv("MIMECRYPT_STATE_DIR", t.TempDir())

	topologyPath := filepath.Join(t.TempDir(), "topology.json")
	cmd := newConfigSourceCmd()
	cmd.SetArgs([]string{"--topology-file", topologyPath})
	cmd.SetIn(strings.NewReader("q\n"))

	output, err := captureCommandStdout(t, func() error {
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output, "已取消 source 配置") {
		t.Fatalf("output = %q, want cancel message", output)
	}
	if _, statErr := os.Stat(topologyPath); !os.IsNotExist(statErr) {
		t.Fatalf("topology should not be written, stat err = %v", statErr)
	}
}
