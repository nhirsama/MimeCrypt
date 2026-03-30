package graph

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

type recordingScopedSession struct {
	accessCalls int
	scopedCalls int
	scopes      [][]string
}

func (s *recordingScopedSession) Login(context.Context, io.Writer) (provider.Token, error) {
	return provider.Token{}, nil
}

func (s *recordingScopedSession) AccessToken(context.Context) (string, error) {
	s.accessCalls++
	return "fallback-token", nil
}

func (s *recordingScopedSession) AccessTokenForScopes(_ context.Context, scopes []string) (string, error) {
	s.scopedCalls++
	s.scopes = append(s.scopes, append([]string(nil), scopes...))
	return "scoped-token", nil
}

func (s *recordingScopedSession) LoadCachedToken() (provider.Token, error) {
	return provider.Token{}, nil
}

func (s *recordingScopedSession) Logout() error {
	return nil
}

func TestBuildSourceClientsWithSessionUsesScopedGraphToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer scoped-token" {
			t.Fatalf("Authorization = %q, want Bearer scoped-token", got)
		}
		_, _ = io.WriteString(w, `{"id":"u1","mail":"user@example.com","displayName":"User"}`)
	}))
	defer server.Close()

	cfg := testGraphFactoryConfig(server.URL)
	session := &recordingScopedSession{}
	clients, err := BuildSourceClientsWithSession(cfg, session)
	if err != nil {
		t.Fatalf("BuildSourceClientsWithSession() error = %v", err)
	}

	if _, err := clients.Reader.Me(context.Background()); err != nil {
		t.Fatalf("Reader.Me() error = %v", err)
	}
	if session.accessCalls != 0 {
		t.Fatalf("AccessToken() calls = %d, want 0", session.accessCalls)
	}
	if session.scopedCalls != 1 {
		t.Fatalf("AccessTokenForScopes() calls = %d, want 1", session.scopedCalls)
	}
	if want := []string{"scope-graph"}; !reflect.DeepEqual(session.scopes[0], want) {
		t.Fatalf("scopes = %#v, want %#v", session.scopes[0], want)
	}
}

func TestNewWriterClientsUsesScopedGraphToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer scoped-token" {
			t.Fatalf("Authorization = %q, want Bearer scoped-token", got)
		}
		_, _ = io.WriteString(w, `{"id":"u1","mail":"user@example.com","userPrincipalName":"user@example.com"}`)
	}))
	defer server.Close()

	cfg := testGraphFactoryConfig(server.URL)
	session := &recordingScopedSession{}
	clients, err := NewWriterClients(cfg, session)
	if err != nil {
		t.Fatalf("NewWriterClients() error = %v", err)
	}

	if _, err := clients.Health.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if clients.Reconciler == nil {
		t.Fatalf("Reconciler = nil")
	}
	if session.accessCalls != 0 {
		t.Fatalf("AccessToken() calls = %d, want 0", session.accessCalls)
	}
	if session.scopedCalls != 1 {
		t.Fatalf("AccessTokenForScopes() calls = %d, want 1", session.scopedCalls)
	}
	if want := []string{"scope-graph"}; !reflect.DeepEqual(session.scopes[0], want) {
		t.Fatalf("scopes = %#v, want %#v", session.scopes[0], want)
	}
}

func testGraphFactoryConfig(serverURL string) appconfig.Config {
	return appconfig.Config{
		Auth: appconfig.AuthConfig{
			GraphScopes: []string{"scope-graph"},
		},
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{
				GraphBaseURL: serverURL + "/v1.0",
			},
		},
	}
}
