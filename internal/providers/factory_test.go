package providers

import (
	"context"
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

func TestSessionAuthConfigForDriversUsesLeastPrivilegeUnion(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	got := SessionAuthConfigForDrivers(cfg, "imap", "ews")

	if !reflect.DeepEqual(got.GraphScopes, []string{"scope-graph"}) {
		t.Fatalf("GraphScopes = %#v", got.GraphScopes)
	}
	if !reflect.DeepEqual(got.EWSScopes, []string{"scope-ews"}) {
		t.Fatalf("EWSScopes = %#v", got.EWSScopes)
	}
	if !reflect.DeepEqual(got.IMAPScopes, []string{"scope-imap"}) {
		t.Fatalf("IMAPScopes = %#v", got.IMAPScopes)
	}

	got = SessionAuthConfigForDrivers(cfg, "graph")
	if got.EWSScopes != nil || got.IMAPScopes != nil {
		t.Fatalf("unexpected non-graph scopes: %#v", got)
	}
}

func TestBuildSourceClientsWithSessionUsesExplicitDriver(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)
	session, err := auth.NewSession(SessionAuthConfigForDrivers(cfg, "imap"), nil)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	clients, err := BuildSourceClientsWithSession(cfg, "imap", "INBOX", session)
	if err != nil {
		t.Fatalf("BuildSourceClientsWithSession() error = %v", err)
	}
	if got := reflect.TypeOf(clients.Reader).String(); got != "*imap.reader" {
		t.Fatalf("reader type = %s, want *imap.reader", got)
	}
}

func TestBuildSinkClientsUsesExplicitDriver(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)

	clients, err := BuildSinkClients(cfg, "ews", "")
	if err != nil {
		t.Fatalf("BuildSinkClients() error = %v", err)
	}
	if got := reflect.TypeOf(clients.Writer).String(); got != "*graph.ewsWriter" {
		t.Fatalf("writer type = %s, want *graph.ewsWriter", got)
	}
	if clients.Health == nil {
		t.Fatalf("Health = nil")
	}
	if clients.Reconciler == nil {
		t.Fatalf("Reconciler = nil")
	}
}

func TestBuildSinkClientsRejectsLocalConsumerDriver(t *testing.T) {
	t.Parallel()

	cfg := testProviderConfig(t)

	_, err := BuildSinkClients(cfg, "file", "")
	if err == nil || err.Error() != "sink driver file 未提供 provider clients" {
		t.Fatalf("BuildSinkClients() error = %v, want local consumer rejection", err)
	}
}

func TestBuildSinkClientsGraphHealthUsesGraphScopes(t *testing.T) {
	t.Parallel()

	var refreshScope string
	authServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"u1","mail":"user@example.com","userPrincipalName":"user@example.com"}`)
	}))
	defer graphServer.Close()

	cfg := testProviderConfig(t)
	cfg.Auth.AuthorityBaseURL = authServer.URL
	cfg.Mail.Client.GraphBaseURL = graphServer.URL + "/v1.0"

	session, err := auth.NewSession(SessionAuthConfigForDrivers(cfg, "graph"), authServer.Client())
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

	clients, err := BuildSinkClientsWithSession(cfg, "graph", "", session)
	if err != nil {
		t.Fatalf("BuildSinkClientsWithSession() error = %v", err)
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

func testProviderConfig(t *testing.T) appconfig.Config {
	t.Helper()

	return appconfig.Config{
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
			Sync: appconfig.MailSyncConfig{},
		},
	}
}
