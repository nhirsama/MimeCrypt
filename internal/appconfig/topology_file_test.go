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
