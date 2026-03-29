package providers

import (
	"fmt"
	"testing"

	"mimecrypt/internal/appconfig"
)

func TestBuildUsesConfiguredWriteBackProvider(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		sourceProvider    string
		writeBackProvider string
		wantWriterType    string
	}{
		{
			name:              "imap source with graph writer",
			sourceProvider:    "imap",
			writeBackProvider: "graph",
			wantWriterType:    "*graph.writer",
		},
		{
			name:              "imap source with ews writer",
			sourceProvider:    "imap",
			writeBackProvider: "ews",
			wantWriterType:    "*graph.ewsWriter",
		},
		{
			name:              "graph source with imap writer",
			sourceProvider:    "graph",
			writeBackProvider: "imap",
			wantWriterType:    "*imap.writer",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := testProviderConfig(t, tc.sourceProvider, tc.writeBackProvider)
			clients, err := Build(cfg)
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			if got := fmt.Sprintf("%T", clients.Writer); got != tc.wantWriterType {
				t.Fatalf("Writer type = %s, want %s", got, tc.wantWriterType)
			}
		})
	}
}

func testProviderConfig(t *testing.T, providerName, writeBackProvider string) appconfig.Config {
	t.Helper()

	return appconfig.Config{
		Provider: providerName,
		Auth: appconfig.AuthConfig{
			ClientID:         "client-id",
			Tenant:           "organizations",
			AuthorityBaseURL: "https://login.microsoftonline.com",
			GraphScopes:      []string{"scope-graph"},
			EWSScopes:        []string{"scope-ews"},
			IMAPScopes:       []string{"scope-imap"},
			StateDir:         t.TempDir(),
			TokenStore:       "file",
		},
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{
				GraphBaseURL: "https://graph.example.com/v1.0",
				EWSBaseURL:   "https://ews.example.com/EWS/Exchange.asmx",
				IMAPAddr:     "imap.example.com:993",
				IMAPUsername: "user@example.com",
			},
			Pipeline: appconfig.MailPipelineConfig{
				WriteBackProvider: writeBackProvider,
			},
			Sync: appconfig.MailSyncConfig{
				Folder: "INBOX",
			},
		},
	}
}
