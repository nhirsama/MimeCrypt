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
	      "state_dir": "/state/flow/default",
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
	if topology.Sources["office"].StatePath != "" {
		t.Fatalf("legacy source state path = %q, want empty", topology.Sources["office"].StatePath)
	}
	if topology.Routes["default"].StateDir != "" {
		t.Fatalf("legacy route state dir = %q, want empty", topology.Routes["default"].StateDir)
	}
}

func TestLoadTopologyFileAllowsLegacyRuntimeFieldsButStripsThem(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "topology.json")
	content := `{
  "sources": {
    "archive": {
      "name": "archive",
      "driver": "imap",
      "mode": "poll",
      "state_path": "/runtime/flow-sync.json"
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
      "source_refs": ["archive"],
      "state_dir": "/runtime/flow-state",
      "targets": [
        {"name": "discard", "sink_ref": "discard", "required": true}
      ]
    }
  },
  "default_source": "archive",
  "default_route": "default"
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	topology, err := LoadTopologyFile(path)
	if err != nil {
		t.Fatalf("LoadTopologyFile() error = %v", err)
	}
	if got := topology.Sources["archive"].StatePath; got != "" {
		t.Fatalf("source state path = %q, want empty", got)
	}
	if got := topology.Routes["default"].StateDir; got != "" {
		t.Fatalf("route state dir = %q, want empty", got)
	}
}

func TestSaveTopologyFileRoundTrips(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "topology.json")
	want := Topology{
		Sources: map[string]Source{
			"incoming": {
				Name:      "incoming",
				Driver:    "webhook",
				Mode:      "push",
				StatePath: "/runtime/flow-sync.json",
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
				StateDir:   "/runtime/flow-state",
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
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(content), "state_path") || strings.Contains(string(content), "state_dir") {
		t.Fatalf("saved topology leaked runtime fields: %s", string(content))
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
	if got.Sources["incoming"].StatePath != "" {
		t.Fatalf("round-tripped source state path = %q, want empty", got.Sources["incoming"].StatePath)
	}
	if got.Routes["default"].StateDir != "" {
		t.Fatalf("round-tripped route state dir = %q, want empty", got.Routes["default"].StateDir)
	}
}
