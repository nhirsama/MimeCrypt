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
	if err := appconfig.SaveLocalConfig(stateDir, appconfig.LocalConfig{LoginConfig: "oauth-device"}); err != nil {
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

func TestFormatTokenMetaIncludesCredentialRuntimeFields(t *testing.T) {
	t.Parallel()

	got := formatTokenMeta(tokenMeta{
		Credential:     "office-auth",
		CredentialKind: "oauth",
		Runtime:        "oauth-device",
		Drivers:        []string{"imap", "graph"},
		StateDir:       "/state",
		TokenStore:     "file",
	})
	if !strings.Contains(got, "credential=office-auth") {
		t.Fatalf("meta = %q", got)
	}
	if !strings.Contains(got, "runtime=oauth-device") {
		t.Fatalf("meta = %q", got)
	}
	if !strings.Contains(got, "drivers=graph,imap") {
		t.Fatalf("meta = %q", got)
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
