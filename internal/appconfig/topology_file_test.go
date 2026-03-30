package appconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadTopologyFileRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "topology.json")
	content := `{
  "sources": {
    "default": {
      "name": "default",
      "driver": "imap",
      "mode": "poll",
      "folder": "INBOX",
      "poll_interval": 60000000000,
      "cycle_timeout": 120000000000,
      "unexpected_field": true
    }
  },
  "sinks": {
    "discard": {
      "name": "discard",
      "driver": "discard"
    }
  },
  "routes": {
    "default": {
      "name": "default",
      "source_refs": ["default"],
      "targets": [
        {"name": "discard", "sink_ref": "discard", "artifact": "primary", "required": true}
      ]
    }
  },
  "default_source": "default",
  "default_route": "default"
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadTopologyFile(path)
	if err == nil || !strings.Contains(err.Error(), "unexpected_field") {
		t.Fatalf("LoadTopologyFile() error = %v, want unknown field rejection", err)
	}
}

func TestLoadTopologyFileParsesStrictSingleDocument(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "topology.json")
	content := `{
  "sources": {
    "default": {
      "name": "default",
      "driver": "imap",
      "mode": "poll",
      "folder": "INBOX",
      "state_path": "/state/source.json",
      "poll_interval": 60000000000,
      "cycle_timeout": 120000000000
    }
  },
  "sinks": {
    "discard": {
      "name": "discard",
      "driver": "discard"
    }
  },
  "routes": {
    "default": {
      "name": "default",
      "source_refs": ["default"],
      "targets": [
        {"name": "discard", "sink_ref": "discard", "artifact": "primary", "required": true}
      ]
    }
  },
  "default_source": "default",
  "default_route": "default"
}
{}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadTopologyFile(path)
	if err == nil || !strings.Contains(err.Error(), "多余的 JSON 内容") {
		t.Fatalf("LoadTopologyFile() error = %v, want trailing JSON rejection", err)
	}
}

func TestLoadTopologyFileNormalizesNames(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "topology.json")
	content := `{
  "sources": {
    "office": {
      "driver": "imap",
      "mode": "poll",
      "folder": "INBOX",
      "state_path": "/state/source.json",
      "poll_interval": 60000000000,
      "cycle_timeout": 120000000000
    }
  },
  "sinks": {
    "discard": {
      "driver": "discard"
    }
  },
  "routes": {
    "default": {
      "source_refs": ["office"],
      "targets": [
        {"name": "discard", "sink_ref": "discard", "artifact": "primary", "required": true}
      ]
    }
  },
  "default_source": "office",
  "default_route": "default"
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	topology, err := LoadTopologyFile(path)
	if err != nil {
		t.Fatalf("LoadTopologyFile() error = %v", err)
	}
	if topology.Sources["office"].Name != "office" {
		t.Fatalf("source name = %q, want office", topology.Sources["office"].Name)
	}
	if topology.Routes["default"].Name != "default" {
		t.Fatalf("route name = %q, want default", topology.Routes["default"].Name)
	}
	if topology.Sources["office"].PollInterval != time.Minute {
		t.Fatalf("poll interval = %s, want %s", topology.Sources["office"].PollInterval, time.Minute)
	}
}

func TestSaveTopologyFileRoundTrips(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "topology.json")
	want := Topology{
		Sources: map[string]Source{
			"incoming": {
				Name:   "incoming",
				Driver: "webhook",
				Mode:   "push",
				Webhook: &WebhookSource{
					ListenAddr:         "127.0.0.1:8080",
					Path:               "/mail/incoming",
					SecretEnv:          "MIMECRYPT_WEBHOOK_SECRET",
					MaxBodyBytes:       25 << 20,
					TimestampTolerance: 5 * time.Minute,
				},
			},
		},
		Sinks: map[string]Sink{
			"archive": {Name: "archive", Driver: "file", OutputDir: "/tmp/out"},
		},
		Routes: map[string]Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"incoming"},
				Targets: []RouteTarget{
					{Name: "archive", SinkRef: "archive", Artifact: "primary", Required: true},
				},
			},
		},
		DefaultSource: "incoming",
		DefaultRoute:  "default",
	}

	if err := SaveTopologyFile(path, want); err != nil {
		t.Fatalf("SaveTopologyFile() error = %v", err)
	}

	got, err := LoadTopologyFile(path)
	if err != nil {
		t.Fatalf("LoadTopologyFile() error = %v", err)
	}
	if got.DefaultSource != want.DefaultSource || got.DefaultRoute != want.DefaultRoute {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
	if got.Sources["incoming"].Webhook == nil || got.Sources["incoming"].Webhook.Path != "/mail/incoming" {
		t.Fatalf("sources = %+v", got.Sources)
	}
}
