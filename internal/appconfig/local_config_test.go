package appconfig

import (
	"errors"
	"os"
	"reflect"
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
		Drivers:      []string{"IMAP", "graph", "imap"},
		LoginConfig:  " microsoft-oauth ",
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
	if !reflect.DeepEqual(got.Drivers, []string{"graph", "imap"}) {
		t.Fatalf("Drivers = %#v, want [graph imap]", got.Drivers)
	}
	if got.LoginConfig != "microsoft-oauth" {
		t.Fatalf("LoginConfig = %q, want microsoft-oauth", got.LoginConfig)
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
