package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
)

func TestTokenStatusCommandFallsBackWithoutTopology(t *testing.T) {
	stateDir := t.TempDir()
	if err := appconfig.SaveLocalConfig(stateDir, appconfig.LocalConfig{
		RuntimeName: "oauth-device",
		AuthProfile: "imap",
	}); err != nil {
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

func TestTokenStatusCommandRejectsBindingsWithoutRuntimeConfig(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	topologyPath := filepath.Join(stateDir, "topology.json")
	if err := appconfig.SaveTopologyFile(topologyPath, appconfig.Topology{
		DefaultCredential: "office-auth",
		Credentials: map[string]appconfig.Credential{
			"office-auth": {Name: "office-auth", Kind: "oauth"},
		},
		Sources: map[string]appconfig.Source{
			"office": {
				Name:          "office",
				Driver:        "imap",
				Mode:          "poll",
				CredentialRef: "office-auth",
				Folder:        "INBOX",
				PollInterval:  time.Minute,
				CycleTimeout:  2 * time.Minute,
			},
		},
		Sinks: map[string]appconfig.Sink{
			"discard": {Name: "discard", Driver: "discard"},
		},
		Routes: map[string]appconfig.Route{
			"default": {
				Name:       "default",
				SourceRefs: []string{"office"},
				Targets: []appconfig.RouteTarget{
					{Name: "discard", SinkRef: "discard", Required: true},
				},
			},
		},
	}); err != nil {
		t.Fatalf("SaveTopologyFile() error = %v", err)
	}

	cmd := newTokenStatusCmd(appconfig.Config{
		TopologyPath: topologyPath,
		Auth: appconfig.AuthConfig{
			StateDir: stateDir,
		},
	})
	cmd.SetArgs([]string{"--credential", "office-auth"})

	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "未配置运行时驱动") {
		t.Fatalf("Execute() error = %v, want missing runtime config", err)
	}
}

func TestTokenStatusCommandAcceptsLegacyStoredRuntimeName(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	if err := appconfig.SaveLocalConfig(stateDir, appconfig.LocalConfig{
		LoginConfig: "microsoft-oauth",
		Drivers:     []string{"imap"},
	}); err != nil {
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
	if !strings.Contains(output, "runtime=oauth-device") {
		t.Fatalf("output = %q, want normalized runtime", output)
	}
}

func TestFormatTokenMetaIncludesCredentialRuntimeFields(t *testing.T) {
	t.Parallel()

	got := formatTokenMeta(tokenMeta{
		Credential:     "office-auth",
		CredentialKind: "oauth",
		Runtime:        "oauth-device",
		AuthProfile:    "graph+imap",
		StateDir:       "/state",
		TokenStore:     "file",
	})
	if !strings.Contains(got, "credential=office-auth") {
		t.Fatalf("meta = %q", got)
	}
	if !strings.Contains(got, "runtime=oauth-device") {
		t.Fatalf("meta = %q", got)
	}
	if !strings.Contains(got, "auth_profile=graph+imap") {
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
