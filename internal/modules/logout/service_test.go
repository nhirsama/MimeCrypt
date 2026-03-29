package logout

import (
	"context"
	"errors"
	"io"
	"testing"

	"mimecrypt/internal/provider"
)

type fakeSession struct {
	logoutCalled bool
	logoutErr    error
}

func (f *fakeSession) Login(context.Context, io.Writer) (provider.Token, error) {
	return provider.Token{}, nil
}

func (f *fakeSession) AccessToken(context.Context) (string, error) {
	return "", nil
}

func (f *fakeSession) AccessTokenForScopes(context.Context, []string) (string, error) {
	return "", nil
}

func (f *fakeSession) LoadCachedToken() (provider.Token, error) {
	return provider.Token{}, nil
}

func (f *fakeSession) Logout() error {
	f.logoutCalled = true
	return f.logoutErr
}

func TestServiceRunCallsSessionLogout(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	service := Service{Session: session}
	if err := service.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !session.logoutCalled {
		t.Fatalf("expected Session.Logout() to be called")
	}
}

func TestServiceRunRejectsNilSession(t *testing.T) {
	t.Parallel()

	service := Service{}
	if err := service.Run(); err == nil {
		t.Fatalf("Run() error = nil, want validation error")
	}
}

func TestServiceRunPropagatesLogoutError(t *testing.T) {
	t.Parallel()

	session := &fakeSession{logoutErr: errors.New("keyring unavailable")}
	service := Service{Session: session}
	if err := service.Run(); err == nil {
		t.Fatalf("Run() error = nil, want logout error")
	}
}
