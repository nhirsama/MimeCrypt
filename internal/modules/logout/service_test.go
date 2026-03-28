package logout

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceRunRemovesAllTokenPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "token.json"),
		filepath.Join(dir, "graph-token.json"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("token"), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	service := Service{TokenPaths: paths}
	if err := service.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %q to be removed, stat err = %v", path, err)
		}
	}
}

func TestServiceRunRejectsEmptyTokenPaths(t *testing.T) {
	t.Parallel()

	service := Service{}
	if err := service.Run(); err == nil {
		t.Fatalf("Run() error = nil, want validation error")
	}
}
