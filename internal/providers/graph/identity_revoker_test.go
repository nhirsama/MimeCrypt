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

type fakeRevokeSession struct {
	scopes []string
}

func (f *fakeRevokeSession) Login(context.Context, io.Writer) (provider.Token, error) {
	return provider.Token{}, nil
}

func (f *fakeRevokeSession) AccessToken(context.Context) (string, error) {
	return "", nil
}

func (f *fakeRevokeSession) AccessTokenForScopes(context.Context, []string) (string, error) {
	panic("AccessTokenForScopes should be overridden")
}

func (f *fakeRevokeSession) LoadCachedToken() (provider.Token, error) {
	return provider.Token{}, nil
}

func (f *fakeRevokeSession) Logout() error {
	return nil
}

type capturingRevokeSession struct {
	fakeRevokeSession
	token string
}

func (f *capturingRevokeSession) AccessTokenForScopes(_ context.Context, scopes []string) (string, error) {
	f.scopes = append([]string(nil), scopes...)
	return f.token, nil
}

func TestIdentityRevokerUsesGraphRevokeEndpoint(t *testing.T) {
	t.Parallel()

	session := &capturingRevokeSession{token: "revoke-token"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1.0/me/revokeSignInSessions" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer revoke-token" {
			t.Fatalf("Authorization = %q, want Bearer revoke-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"value":true}`)
	}))
	defer server.Close()

	revoker, err := NewIdentityRevoker(appconfig.Config{
		Auth: appconfig.AuthConfig{
			GraphScopes: []string{"scope-graph"},
		},
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{
				GraphBaseURL: server.URL + "/v1.0",
			},
		},
	}, session, server.Client())
	if err != nil {
		t.Fatalf("NewIdentityRevoker() error = %v", err)
	}

	if err := revoker.Revoke(context.Background(), io.Discard); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if !reflect.DeepEqual(session.scopes, []string{"scope-graph", revokeSessionsScope}) {
		t.Fatalf("scopes = %#v", session.scopes)
	}
}

func TestIdentityRevokerRejectsNegativeGraphResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"value":false}`)
	}))
	defer server.Close()

	revoker, err := NewIdentityRevoker(appconfig.Config{
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{
				GraphBaseURL: server.URL + "/v1.0",
			},
		},
	}, &capturingRevokeSession{token: "revoke-token"}, server.Client())
	if err != nil {
		t.Fatalf("NewIdentityRevoker() error = %v", err)
	}

	if err := revoker.Revoke(context.Background(), io.Discard); err == nil {
		t.Fatalf("Revoke() error = nil, want negative response error")
	}
}
