package login

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"mimecrypt/internal/provider"
)

type fakeSession struct {
	loginToken provider.Token
	loginErr   error
	loggedIn   bool
}

func (f *fakeSession) Login(context.Context, io.Writer) (provider.Token, error) {
	if f.loginErr != nil {
		return provider.Token{}, f.loginErr
	}
	f.loggedIn = true
	return f.loginToken, nil
}

func TestServiceRunReturnsIdentityProbeResult(t *testing.T) {
	t.Parallel()

	session := &fakeSession{loginToken: provider.Token{AccessToken: "token"}}
	service := &Service{
		Session:  session,
		StateDir: "/state",
		IdentityProbe: func(context.Context) (provider.User, error) {
			return provider.User{
				Mail:        "user@example.com",
				DisplayName: "User",
			}, nil
		},
	}

	result, err := service.Run(context.Background(), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !session.loggedIn {
		t.Fatalf("expected Login() to be called")
	}
	if result.Account != "user@example.com" || result.DisplayName != "User" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.StateDir != "/state" {
		t.Fatalf("StateDir = %q, want /state", result.StateDir)
	}
}

func TestServiceRunSucceedsWithoutIdentityProbe(t *testing.T) {
	t.Parallel()

	service := &Service{
		Session:  &fakeSession{loginToken: provider.Token{AccessToken: "token"}},
		StateDir: "/state",
	}

	result, err := service.Run(context.Background(), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Account != "" || result.DisplayName != "" {
		t.Fatalf("unexpected identity result: %+v", result)
	}
}

func TestServiceRunReturnsIdentityProbeError(t *testing.T) {
	t.Parallel()

	service := &Service{
		Session: &fakeSession{loginToken: provider.Token{AccessToken: "token"}},
		IdentityProbe: func(context.Context) (provider.User, error) {
			return provider.User{}, errors.New("probe failed")
		},
	}

	_, err := service.Run(context.Background(), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "验证当前用户信息失败") {
		t.Fatalf("Run() error = %v, want identity probe error", err)
	}
}
