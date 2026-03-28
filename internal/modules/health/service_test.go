package health

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"mimecrypt/internal/provider"
)

type fakeSession struct {
	loadToken   provider.Token
	loadErr     error
	accessToken string
	accessErr   error
}

func (f fakeSession) Login(context.Context, io.Writer) (provider.Token, error) {
	return provider.Token{}, nil
}

func (f fakeSession) AccessToken(context.Context) (string, error) {
	return f.accessToken, f.accessErr
}

func (f fakeSession) LoadCachedToken() (provider.Token, error) {
	return f.loadToken, f.loadErr
}

func (f fakeSession) Logout() error {
	return nil
}

type fakeReader struct {
	meUser    provider.User
	meErr     error
	listErr   error
	listCalls int
	meCalls   int
}

func (f *fakeReader) Me(context.Context) (provider.User, error) {
	f.meCalls++
	return f.meUser, f.meErr
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
	f.listCalls++
	return nil, f.listErr
}

func TestServiceRunGraphChecksAllHealthy(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{meUser: provider.User{Mail: "user@example.com"}}
	service := Service{
		StateDir: t.TempDir(),
		Provider: "graph",
		Session: fakeSession{
			loadToken:   provider.Token{ExpiresAt: time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)},
			accessToken: "access-token",
		},
		Reader: reader,
		LookPath: func(string) (string, error) {
			return "/usr/bin/gpg", nil
		},
	}

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.OK() {
		t.Fatalf("Run() = %+v, want all checks OK", result)
	}
	if reader.meCalls != 1 {
		t.Fatalf("Me() calls = %d, want 1", reader.meCalls)
	}
}

func TestServiceRunIMAPUsesFolderProbe(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{}
	service := Service{
		StateDir: t.TempDir(),
		Provider: "imap",
		Folder:   "INBOX",
		Session: fakeSession{
			loadToken:   provider.Token{ExpiresAt: time.Now().Add(time.Hour)},
			accessToken: "access-token",
		},
		Reader: reader,
		LookPath: func(string) (string, error) {
			return "/usr/bin/gpg", nil
		},
	}

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.OK() {
		t.Fatalf("Run() = %+v, want all checks OK", result)
	}
	if reader.listCalls != 1 {
		t.Fatalf("LatestMessagesInFolder() calls = %d, want 1", reader.listCalls)
	}
	if reader.meCalls != 0 {
		t.Fatalf("Me() calls = %d, want 0", reader.meCalls)
	}
}

func TestServiceRunReportsFailedChecks(t *testing.T) {
	t.Parallel()

	service := Service{
		StateDir: t.TempDir(),
		Provider: "graph",
		Session: fakeSession{
			loadErr:   errors.New("missing token"),
			accessErr: errors.New("refresh failed"),
		},
		Reader: &fakeReader{meErr: errors.New("provider unavailable")},
		LookPath: func(string) (string, error) {
			return "", errors.New("gpg not found")
		},
	}

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.OK() {
		t.Fatalf("Run() = %+v, want failed checks", result)
	}
	if got := FormatText(result); got == "" {
		t.Fatalf("FormatText() returned empty string")
	}
}
