package tokenstate

import (
	"errors"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
)

type fakeSession struct {
	token     provider.Token
	loadErr   error
	storeErr  error
	stored    provider.Token
	storeCall int
}

func (f *fakeSession) LoadCachedToken() (provider.Token, error) {
	return f.token, f.loadErr
}

func (f *fakeSession) StoreToken(token provider.Token) error {
	f.storeCall++
	f.stored = token
	return f.storeErr
}

func TestServiceStatusReturnsAbsentWhenLoginMissing(t *testing.T) {
	t.Parallel()

	service := Service{
		Credential:     "office-auth",
		CredentialKind: "oauth",
		Runtime:        "oauth-device",
		AuthProfile:    "imap",
		Session:        &fakeSession{loadErr: auth.ErrLoginRequired},
		StateDir:       "/state",
		TokenStore:     "file",
	}

	result, err := service.Status()
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if result.Present {
		t.Fatalf("Present = true, want false")
	}
	if result.Credential != "office-auth" || result.Runtime != "oauth-device" {
		t.Fatalf("unexpected metadata: %+v", result)
	}
	if result.AuthProfile != "imap" {
		t.Fatalf("AuthProfile = %q, want imap", result.AuthProfile)
	}
}

func TestServiceStatusReturnsTokenMetadata(t *testing.T) {
	t.Parallel()

	token := provider.Token{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC),
	}
	service := Service{
		Session:    &fakeSession{token: token},
		StateDir:   "/state",
		TokenStore: "file",
	}

	result, err := service.Status()
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !result.Present {
		t.Fatalf("Present = false, want true")
	}
	if result.Token.RefreshToken != "refresh" {
		t.Fatalf("RefreshToken = %q, want refresh", result.Token.RefreshToken)
	}
}

func TestServiceImportStoresTokenFromJSON(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	service := Service{Session: session, StateDir: "/state", TokenStore: "file"}

	result, err := service.Import(strings.NewReader(`{"access_token":"a","refresh_token":"r","expires_at":"2026-03-28T12:00:00Z"}`))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if session.storeCall != 1 {
		t.Fatalf("StoreToken() calls = %d, want 1", session.storeCall)
	}
	if result.Token.RefreshToken != "r" {
		t.Fatalf("RefreshToken = %q, want r", result.Token.RefreshToken)
	}
}

func TestServiceImportRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	service := Service{Session: &fakeSession{}}
	if _, err := service.Import(strings.NewReader("not-json")); err == nil {
		t.Fatalf("Import() error = nil, want parse error")
	}
}

func TestServiceImportPropagatesStoreError(t *testing.T) {
	t.Parallel()

	service := Service{
		Session: &fakeSession{storeErr: errors.New("write failed")},
	}
	if _, err := service.Import(strings.NewReader(`{"refresh_token":"r"}`)); err == nil {
		t.Fatalf("Import() error = nil, want store error")
	}
}
