package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"mimecrypt/internal/appconfig"
)

func TestTokenStatusCommandFallsBackWithoutTopology(t *testing.T) {
	stateDir := t.TempDir()
	if err := appconfig.SaveLocalConfig(stateDir, appconfig.LocalConfig{Drivers: []string{"imap"}}); err != nil {
		t.Fatalf("SaveLocalConfig() error = %v", err)
	}
	cmd := newTokenStatusCmd(appconfig.Config{
		TopologyPath: appconfig.DefaultTopologyPath(stateDir),
		Auth: appconfig.AuthConfig{
			ClientID:         "client-id",
			Tenant:           "organizations",
			AuthorityBaseURL: "https://login.microsoftonline.com",
			IMAPScopes:       []string{"scope-imap"},
			StateDir:         stateDir,
			TokenStore:       "file",
		},
	})
	cmd.SetArgs(nil)

	output, err := captureCommandStdout(t, func() error {
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output, "token_absent") {
		t.Fatalf("output = %q, want token_absent", output)
	}
	if !strings.Contains(output, "state_dir="+stateDir) {
		t.Fatalf("output = %q, want scoped state dir", output)
	}
}

func captureCommandStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := run()
	_ = writer.Close()

	content, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatalf("ReadAll() error = %v", readErr)
	}
	return string(content), runErr
}
