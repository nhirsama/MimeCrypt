package health

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"mimecrypt/internal/provider"
)

const envGPGBinary = "MIMECRYPT_GPG_BINARY"

type Check struct {
	Name   string
	OK     bool
	Detail string
}

type Result struct {
	Checks []Check
}

func (r Result) OK() bool {
	for _, check := range r.Checks {
		if !check.OK {
			return false
		}
	}
	return len(r.Checks) > 0
}

type Service struct {
	StateDir string
	Folder   string
	Provider string
	Session  provider.Session
	Reader   provider.Reader
	LookPath func(string) (string, error)
	Getenv   func(string) string
}

func (s *Service) Run(ctx context.Context) (Result, error) {
	if s == nil {
		return Result{}, fmt.Errorf("health service 不能为空")
	}
	if s.Session == nil {
		return Result{}, fmt.Errorf("health session 不能为空")
	}
	if s.Reader == nil {
		return Result{}, fmt.Errorf("health reader 不能为空")
	}
	if strings.TrimSpace(s.StateDir) == "" {
		return Result{}, fmt.Errorf("health state dir 不能为空")
	}

	checks := []Check{
		s.checkStateDir(),
		s.checkGPG(),
		s.checkCachedToken(),
		s.checkAccessToken(ctx),
		s.checkProvider(ctx),
	}
	return Result{Checks: checks}, nil
}

func (s *Service) checkStateDir() Check {
	stateDir := strings.TrimSpace(s.StateDir)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return Check{Name: "state_dir", Detail: fmt.Sprintf("不可创建: %v", err)}
	}
	file, err := os.CreateTemp(stateDir, "health-*.tmp")
	if err != nil {
		return Check{Name: "state_dir", Detail: fmt.Sprintf("不可写: %v", err)}
	}
	path := file.Name()
	_ = file.Close()
	_ = os.Remove(path)
	return Check{Name: "state_dir", OK: true, Detail: stateDir}
}

func (s *Service) checkGPG() Check {
	binary := "gpg"
	if value := strings.TrimSpace(s.getenv(envGPGBinary)); value != "" {
		binary = value
	}
	path, err := s.lookPath(binary)
	if err != nil {
		return Check{Name: "gpg", Detail: err.Error()}
	}
	return Check{Name: "gpg", OK: true, Detail: path}
}

func (s *Service) checkCachedToken() Check {
	token, err := s.Session.LoadCachedToken()
	if err != nil {
		return Check{Name: "cached_token", Detail: err.Error()}
	}
	detail := "已加载"
	if !token.ExpiresAt.IsZero() {
		detail = "expires_at=" + token.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return Check{Name: "cached_token", OK: true, Detail: detail}
}

func (s *Service) checkAccessToken(ctx context.Context) Check {
	token, err := s.Session.AccessToken(ctx)
	if err != nil {
		return Check{Name: "access_token", Detail: err.Error()}
	}
	return Check{Name: "access_token", OK: true, Detail: fmt.Sprintf("长度=%d", len(token))}
}

func (s *Service) checkProvider(ctx context.Context) Check {
	switch strings.ToLower(strings.TrimSpace(s.Provider)) {
	case "imap":
		folder := strings.TrimSpace(s.Folder)
		if folder == "" {
			folder = "INBOX"
		}
		_, err := s.Reader.LatestMessagesInFolder(ctx, folder, 0, 1)
		if err != nil {
			return Check{Name: "provider_probe", Detail: err.Error()}
		}
		return Check{Name: "provider_probe", OK: true, Detail: "imap folder=" + folder}
	default:
		user, err := s.Reader.Me(ctx)
		if err != nil {
			return Check{Name: "provider_probe", Detail: err.Error()}
		}
		return Check{Name: "provider_probe", OK: true, Detail: user.Account()}
	}
}

func (s *Service) lookPath(file string) (string, error) {
	if s != nil && s.LookPath != nil {
		return s.LookPath(file)
	}
	return exec.LookPath(file)
}

func (s *Service) getenv(key string) string {
	if s != nil && s.Getenv != nil {
		return s.Getenv(key)
	}
	return os.Getenv(key)
}

func FormatText(result Result) string {
	var builder strings.Builder
	for _, check := range result.Checks {
		status := "FAIL"
		if check.OK {
			status = "OK"
		}
		builder.WriteString(status)
		builder.WriteString(" ")
		builder.WriteString(check.Name)
		if detail := strings.TrimSpace(check.Detail); detail != "" {
			builder.WriteString(": ")
			builder.WriteString(detail)
		}
		builder.WriteString("\n")
	}
	return strings.TrimRight(builder.String(), "\n")
}
