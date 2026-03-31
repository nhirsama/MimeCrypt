package appconfig

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestLoadLocalConfigNormalizesDriversAndMicrosoftFields(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	if err := SaveLocalConfig(stateDir, LocalConfig{
		RuntimeName:  " oauth-device ",
		AuthProfile:  " imap+graph+imap ",
		IMAPUsername: "",
		Microsoft: &MicrosoftLocalConfig{
			ClientID:         " client-id ",
			Tenant:           " tenant-id ",
			AuthorityBaseURL: " https://login.microsoftonline.com ",
			IMAPUsername:     " mailbox@example.com ",
		},
	}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	got, err := LoadLocalConfig(stateDir)
	if err != nil {
		t.Fatalf("LoadLocalConfig() error = %v", err)
	}
	if !reflect.DeepEqual(got.AuthHintNames(), []string{"graph", "imap"}) {
		t.Fatalf("AuthHintNames = %#v, want [graph imap]", got.AuthHintNames())
	}
	if got.RuntimeName != "oauth-device" {
		t.Fatalf("RuntimeName = %q, want oauth-device", got.RuntimeName)
	}
	if got.AuthProfile != "graph+imap" {
		t.Fatalf("AuthProfile = %q, want graph+imap", got.AuthProfile)
	}
	if got.IMAPUsername != "mailbox@example.com" {
		t.Fatalf("IMAPUsername = %q, want mailbox@example.com", got.IMAPUsername)
	}
	if got.Microsoft == nil {
		t.Fatalf("Microsoft = nil")
	}
	if got.Microsoft.ClientID != "client-id" || got.Microsoft.Tenant != "tenant-id" {
		t.Fatalf("Microsoft = %#v", got.Microsoft)
	}
}

func TestLoadLocalConfigAcceptsLegacyLoginConfigAndDrivers(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	content := `{
  "loginConfig": "oauth-device",
  "drivers": ["imap", "graph", "imap"],
  "microsoft": {
    "imapUsername": "legacy@example.com"
  }
}`
	if err := os.WriteFile(LocalConfigPath(stateDir), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := LoadLocalConfig(stateDir)
	if err != nil {
		t.Fatalf("LoadLocalConfig() error = %v", err)
	}
	if got.RuntimeName != "oauth-device" {
		t.Fatalf("RuntimeName = %q, want oauth-device", got.RuntimeName)
	}
	if got.AuthProfile != "graph+imap" {
		t.Fatalf("AuthProfile = %q, want graph+imap", got.AuthProfile)
	}
	if !reflect.DeepEqual(got.AuthHintNames(), []string{"graph", "imap"}) {
		t.Fatalf("AuthHintNames = %#v, want [graph imap]", got.AuthHintNames())
	}
	if got.IMAPUsername != "legacy@example.com" {
		t.Fatalf("IMAPUsername = %q, want legacy@example.com", got.IMAPUsername)
	}
}

func TestLoadLocalConfigPrefersRuntimeAndAuthProfileOverLegacyFields(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	content := `{
  "runtime": "oauth-device",
  "authProfile": "graph",
  "loginConfig": "microsoft-oauth",
  "drivers": ["imap"]
}`
	if err := os.WriteFile(LocalConfigPath(stateDir), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := LoadLocalConfig(stateDir)
	if err != nil {
		t.Fatalf("LoadLocalConfig() error = %v", err)
	}
	if got.RuntimeName != "oauth-device" {
		t.Fatalf("RuntimeName = %q, want oauth-device", got.RuntimeName)
	}
	if got.AuthProfile != "graph" {
		t.Fatalf("AuthProfile = %q, want graph", got.AuthProfile)
	}
	if !reflect.DeepEqual(got.AuthHintNames(), []string{"graph"}) {
		t.Fatalf("AuthHintNames = %#v, want [graph]", got.AuthHintNames())
	}
}

func TestSaveLocalConfigWritesNewCredentialRuntimeShape(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	cfg := LocalConfig{
		RuntimeName: "oauth-device",
		AuthProfile: "imap+graph",
		Microsoft: &MicrosoftLocalConfig{
			ClientID: "client-id",
		},
	}
	if err := SaveLocalConfig(stateDir, cfg); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(stateDir, "config.json"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(content)
	if !strings.Contains(text, `"runtime": "oauth-device"`) {
		t.Fatalf("saved config = %s", text)
	}
	if !strings.Contains(text, `"authProfile": "graph+imap"`) {
		t.Fatalf("saved config = %s", text)
	}
	if strings.Contains(text, "loginConfig") || strings.Contains(text, `"drivers"`) {
		t.Fatalf("saved config leaked legacy fields: %s", text)
	}
}
