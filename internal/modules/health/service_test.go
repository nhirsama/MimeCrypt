package health

import (
	"context"
	"errors"
	"io"
	"strings"
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

func (f fakeSession) AccessTokenForScopes(context.Context, []string) (string, error) {
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

type fakeWriter struct {
	healthDetail string
	healthErr    error
	healthCalls  int
}

func (f *fakeWriter) WriteMessage(context.Context, provider.WriteRequest) (provider.WriteResult, error) {
	return provider.WriteResult{}, nil
}

func (f *fakeWriter) HealthCheck(context.Context) (string, error) {
	f.healthCalls++
	return f.healthDetail, f.healthErr
}

type writeOnlyWriter struct{}

func (writeOnlyWriter) WriteMessage(context.Context, provider.WriteRequest) (provider.WriteResult, error) {
	return provider.WriteResult{}, nil
}

func TestServiceRunDefaultChecksReadonlyOnly(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{meUser: provider.User{Mail: "user@example.com"}}
	writer := &fakeWriter{healthDetail: "graph"}
	service := Service{
		StateDir: t.TempDir(),
		Provider: "graph",
		Session: fakeSession{
			loadToken: provider.Token{ExpiresAt: time.Now().Add(time.Hour)},
		},
		Reader:    reader,
		WriteBack: writer,
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
	if len(result.Checks) != 3 {
		t.Fatalf("checks = %d, want 3", len(result.Checks))
	}
	if reader.meCalls != 0 {
		t.Fatalf("Me() calls = %d, want 0 in readonly mode", reader.meCalls)
	}
	if reader.listCalls != 0 {
		t.Fatalf("LatestMessagesInFolder() calls = %d, want 0 in readonly mode", reader.listCalls)
	}
	if writer.healthCalls != 0 {
		t.Fatalf("HealthCheck() calls = %d, want 0 in readonly mode", writer.healthCalls)
	}
}

func TestServiceRunDeepUsesReaderAndWriterProbes(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{}
	writer := &fakeWriter{healthDetail: "imap addr=imap.example.com:993"}
	service := Service{
		StateDir:          t.TempDir(),
		Provider:          "imap",
		ProviderProbeKind: provider.ProviderProbeFolderList,
		WriteBackProvider: "imap",
		Folder:            "INBOX",
		Deep:              true,
		Session: fakeSession{
			loadToken: provider.Token{ExpiresAt: time.Now().Add(time.Hour)},
		},
		Reader:    reader,
		WriteBack: writer,
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
	if writer.healthCalls != 1 {
		t.Fatalf("HealthCheck() calls = %d, want 1", writer.healthCalls)
	}
}

func TestServiceRunDeepUsesIdentityProbeKind(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{meUser: provider.User{Mail: "user@example.com"}}
	service := Service{
		StateDir:          t.TempDir(),
		ProviderProbeKind: provider.ProviderProbeIdentity,
		Deep:              true,
		Session: fakeSession{
			loadToken: provider.Token{ExpiresAt: time.Now().Add(time.Hour)},
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
	if reader.listCalls != 0 {
		t.Fatalf("LatestMessagesInFolder() calls = %d, want 0", reader.listCalls)
	}
	if reader.meCalls != 1 {
		t.Fatalf("Me() calls = %d, want 1", reader.meCalls)
	}
}

func TestServiceRunDeepFailsWhenWriterProbeUnsupported(t *testing.T) {
	t.Parallel()

	service := Service{
		StateDir: t.TempDir(),
		Provider: "graph",
		Deep:     true,
		Session: fakeSession{
			loadToken: provider.Token{ExpiresAt: time.Now().Add(time.Hour)},
		},
		Reader: &fakeReader{meUser: provider.User{Mail: "user@example.com"}},
		WriteBacks: []WriteBackProbe{
			{Name: "archive", Driver: "graph", Health: nil},
		},
		LookPath: func(string) (string, error) {
			return "/usr/bin/gpg", nil
		},
	}

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.OK() {
		t.Fatalf("Run() = %+v, want failed checks", result)
	}
	if got := FormatText(result); !strings.Contains(got, "writeback_probe") {
		t.Fatalf("FormatText() = %q, want writeback probe failure", got)
	}
}

func TestServiceRunDeepSupportsMultipleNamedWriteBackProbes(t *testing.T) {
	t.Parallel()

	archiveWriter := &fakeWriter{healthDetail: "imap addr=imap.example.com:993"}
	mirrorWriter := &fakeWriter{healthDetail: "user@example.com"}
	service := Service{
		StateDir: t.TempDir(),
		Provider: "imap",
		Folder:   "INBOX",
		Deep:     true,
		Session: fakeSession{
			loadToken: provider.Token{ExpiresAt: time.Now().Add(time.Hour)},
		},
		Reader: &fakeReader{},
		WriteBacks: []WriteBackProbe{
			{Name: "archive", Driver: "imap", Health: archiveWriter},
			{Name: "mirror", Driver: "graph", Health: mirrorWriter},
		},
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
	if archiveWriter.healthCalls != 1 || mirrorWriter.healthCalls != 1 {
		t.Fatalf("unexpected writeback health calls: archive=%d mirror=%d", archiveWriter.healthCalls, mirrorWriter.healthCalls)
	}
	if got := FormatText(result); !strings.Contains(got, "writeback_probe[archive]") || !strings.Contains(got, "writeback_probe[mirror]") {
		t.Fatalf("FormatText() = %q, want named writeback probes", got)
	}
}

func TestServiceRunFailsCachedTokenWhenExpiredWithoutRefreshToken(t *testing.T) {
	t.Parallel()

	service := Service{
		StateDir: t.TempDir(),
		Session: fakeSession{
			loadToken: provider.Token{ExpiresAt: time.Now().Add(-time.Minute)},
		},
		LookPath: func(string) (string, error) {
			return "/usr/bin/gpg", nil
		},
	}

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.OK() {
		t.Fatalf("Run() = %+v, want failed cached token check", result)
	}
}

func TestServiceRunReportsMultipleFailedChecks(t *testing.T) {
	t.Parallel()

	service := Service{
		StateDir: t.TempDir(),
		Provider: "graph",
		Deep:     true,
		Session: fakeSession{
			loadErr: errors.New("missing token"),
		},
		Reader:    &fakeReader{meErr: errors.New("provider unavailable")},
		WriteBack: &fakeWriter{healthErr: errors.New("writer unavailable")},
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

func TestServiceRunSupportsLocalSourceWithoutSessionOrReader(t *testing.T) {
	t.Parallel()

	service := Service{
		StateDir:          t.TempDir(),
		Deep:              true,
		SkipCachedToken:   true,
		SkipProviderProbe: true,
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
	if len(result.Checks) != 3 {
		t.Fatalf("checks = %d, want 3", len(result.Checks))
	}
}
