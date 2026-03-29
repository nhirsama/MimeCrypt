package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
)

func TestBuildSourceAndWriteBackClientsWithSharedSessionUseConfiguredWriteBackProvider(t *testing.T) {
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
			session, err := auth.NewSession(sessionAuthConfig(cfg), nil)
			if err != nil {
				t.Fatalf("NewSession() error = %v", err)
			}
			source, err := BuildSourceClientsWithSession(cfg, session)
			if err != nil {
				t.Fatalf("BuildSourceClientsWithSession() error = %v", err)
			}
			clients, err := BuildWriteBackClientsWithSession(cfg, session)
			if err != nil {
				t.Fatalf("BuildWriteBackClientsWithSession() error = %v", err)
			}
			if source.Reader == nil {
				t.Fatalf("Source reader = nil")
			}
			if got := fmt.Sprintf("%T", clients.Writer); got != tc.wantWriterType {
				t.Fatalf("Writer type = %s, want %s", got, tc.wantWriterType)
			}
		})
	}
}

func TestSessionAuthConfigScopesFollowSourceAndWriteBackProviders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		sourceProvider    string
		writeBackProvider string
		wantGraph         []string
		wantEWS           []string
		wantIMAP          []string
	}{
		{
			name:              "imap source with graph writer",
			sourceProvider:    "imap",
			writeBackProvider: "graph",
			wantGraph:         []string{"scope-graph"},
			wantIMAP:          []string{"scope-imap"},
		},
		{
			name:              "imap source with ews writer",
			sourceProvider:    "imap",
			writeBackProvider: "ews",
			wantGraph:         []string{"scope-graph"},
			wantEWS:           []string{"scope-ews"},
			wantIMAP:          []string{"scope-imap"},
		},
		{
			name:              "graph source with imap writer",
			sourceProvider:    "graph",
			writeBackProvider: "imap",
			wantGraph:         []string{"scope-graph"},
			wantIMAP:          []string{"scope-imap"},
		},
		{
			name:              "graph source with graph writer",
			sourceProvider:    "graph",
			writeBackProvider: "graph",
			wantGraph:         []string{"scope-graph"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := testProviderConfig(t, tc.sourceProvider, tc.writeBackProvider)
			got := sessionAuthConfig(cfg)
			if !reflect.DeepEqual(got.GraphScopes, tc.wantGraph) {
				t.Fatalf("GraphScopes = %#v, want %#v", got.GraphScopes, tc.wantGraph)
			}
			if !reflect.DeepEqual(got.EWSScopes, tc.wantEWS) {
				t.Fatalf("EWSScopes = %#v, want %#v", got.EWSScopes, tc.wantEWS)
			}
			if !reflect.DeepEqual(got.IMAPScopes, tc.wantIMAP) {
				t.Fatalf("IMAPScopes = %#v, want %#v", got.IMAPScopes, tc.wantIMAP)
			}
		})
	}
}

func TestBuildWriteBackClientsExposeExplicitCapabilities(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		sourceProvider    string
		writeBackProvider string
		wantReconciler    bool
		wantHealth        bool
	}{
		{
			name:              "graph writer exposes health only",
			sourceProvider:    "imap",
			writeBackProvider: "graph",
			wantHealth:        true,
		},
		{
			name:              "imap writer exposes health and reconciler",
			sourceProvider:    "graph",
			writeBackProvider: "imap",
			wantReconciler:    true,
			wantHealth:        true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := testProviderConfig(t, tc.sourceProvider, tc.writeBackProvider)
			clients, err := BuildWriteBackClients(cfg)
			if err != nil {
				t.Fatalf("BuildWriteBackClients() error = %v", err)
			}
			if clients.Writer == nil {
				t.Fatalf("Writer = nil")
			}
			if clients.Reader == nil {
				t.Fatalf("Reader = nil")
			}
			if (clients.Reconciler != nil) != tc.wantReconciler {
				t.Fatalf("Reconciler present = %t, want %t", clients.Reconciler != nil, tc.wantReconciler)
			}
			if (clients.Health != nil) != tc.wantHealth {
				t.Fatalf("Health present = %t, want %t", clients.Health != nil, tc.wantHealth)
			}
		})
	}
}

func TestBuildWriteBackClientsMixedProviderGraphHealthUsesGraphScopes(t *testing.T) {
	t.Parallel()

	var refreshScope string
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/organizations/oauth2/v2.0/token" {
			t.Fatalf("unexpected auth request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		refreshScope = r.Form.Get("scope")
		_, _ = io.WriteString(w, `{"access_token":"access-graph","refresh_token":"refresh-next","token_type":"Bearer","scope":"scope-graph","expires_in":3600}`)
	}))
	defer authServer.Close()

	graphServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1.0/me") {
			t.Fatalf("unexpected graph request path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-graph" {
			t.Fatalf("Authorization = %q, want Bearer access-graph", got)
		}
		_, _ = io.WriteString(w, `{"id":"u1","mail":"user@example.com","userPrincipalName":"user@example.com"}`)
	}))
	defer graphServer.Close()

	cfg := testProviderConfig(t, "imap", "graph")
	cfg.Auth.AuthorityBaseURL = authServer.URL
	cfg.Mail.Client.GraphBaseURL = graphServer.URL + "/v1.0"

	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := session.StoreToken(auth.Token{
		AccessToken:  "stale-imap",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Scope:        "scope-imap",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("StoreToken() error = %v", err)
	}

	clients, err := BuildWriteBackClients(cfg)
	if err != nil {
		t.Fatalf("BuildWriteBackClients() error = %v", err)
	}
	if clients.Health == nil {
		t.Fatalf("Health = nil")
	}

	detail, err := clients.Health.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if detail != "user@example.com" {
		t.Fatalf("detail = %q, want user@example.com", detail)
	}
	if refreshScope != "scope-graph" {
		t.Fatalf("refresh scope = %q, want scope-graph", refreshScope)
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
