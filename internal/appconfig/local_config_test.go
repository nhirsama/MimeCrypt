package appconfig

import (
	"errors"
	"os"
	"testing"
)

func TestClearLocalConfigRemovesConfigFile(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	if err := SaveLocalConfig(stateDir, LocalConfig{IMAPUsername: "user@example.com"}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	if err := ClearLocalConfig(stateDir); err != nil {
		t.Fatalf("ClearLocalConfig() error = %v", err)
	}
	if _, err := os.Stat(LocalConfigPath(stateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat() error = %v, want os.ErrNotExist", err)
	}
}

func TestClearLocalConfigIgnoresMissingFile(t *testing.T) {
	t.Parallel()

	if err := ClearLocalConfig(t.TempDir()); err != nil {
		t.Fatalf("ClearLocalConfig() error = %v", err)
	}
}
