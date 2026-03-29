package login

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"mimecrypt/internal/provider"
)

type fakeSession struct {
	loginCalled bool
	loginOut    io.Writer
	loginErr    error
}

func (f *fakeSession) Login(_ context.Context, out io.Writer) (provider.Token, error) {
	f.loginCalled = true
	f.loginOut = out
	if f.loginErr != nil {
		return provider.Token{}, f.loginErr
	}
	return provider.Token{AccessToken: "token"}, nil
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
	return nil
}

type fakeReader struct {
	meCalled bool
	user     provider.User
	err      error
}

func (f *fakeReader) Me(context.Context) (provider.User, error) {
	f.meCalled = true
	if f.err != nil {
		return provider.User{}, f.err
	}
	return f.user, nil
}

func (f *fakeReader) Message(context.Context, string) (provider.Message, error) {
	return provider.Message{}, nil
}

func (f *fakeReader) FetchMIME(context.Context, string) (io.ReadCloser, error) {
	return nil, nil
}

func (f *fakeReader) DeltaCreatedMessages(context.Context, string, string) ([]provider.Message, string, error) {
	return nil, "", nil
}

func (f *fakeReader) FirstMessageInFolder(context.Context, string) (provider.Message, bool, error) {
	return provider.Message{}, false, nil
}

func (f *fakeReader) LatestMessagesInFolder(context.Context, string, int, int) ([]provider.Message, error) {
	return nil, nil
}

func TestRunReturnsAccountAndStateDir(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	reader := &fakeReader{
		user: provider.User{
			Mail:        "user@example.com",
			DisplayName: "Test User",
		},
	}
	service := Service{
		Session:  session,
		Mail:     reader,
		StateDir: "/state",
	}

	var out strings.Builder
	result, err := service.Run(context.Background(), &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !session.loginCalled {
		t.Fatalf("expected Login() to be called")
	}
	if session.loginOut != &out {
		t.Fatalf("expected login output writer to be forwarded")
	}
	if !reader.meCalled {
		t.Fatalf("expected Me() to be called")
	}
	if result.Account != "user@example.com" {
		t.Fatalf("Account = %q, want user@example.com", result.Account)
	}
	if result.DisplayName != "Test User" {
		t.Fatalf("DisplayName = %q, want Test User", result.DisplayName)
	}
	if result.StateDir != "/state" {
		t.Fatalf("StateDir = %q, want /state", result.StateDir)
	}
}

func TestRunReturnsLoginError(t *testing.T) {
	t.Parallel()

	session := &fakeSession{loginErr: errors.New("login failed")}
	reader := &fakeReader{}
	service := Service{
		Session: session,
		Mail:    reader,
	}

	_, err := service.Run(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "login failed") {
		t.Fatalf("Run() error = %v, want login failure", err)
	}
	if reader.meCalled {
		t.Fatalf("Me() should not be called after login failure")
	}
}

func TestRunWrapsCurrentUserLookupError(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	reader := &fakeReader{err: errors.New("graph unavailable")}
	service := Service{
		Session: session,
		Mail:    reader,
	}

	_, err := service.Run(context.Background(), io.Discard)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "登录成功，但验证当前用户信息失败") {
		t.Fatalf("error = %q, want wrapped user lookup failure", err)
	}
	if !strings.Contains(err.Error(), "graph unavailable") {
		t.Fatalf("error = %q, want original cause", err)
	}
}
