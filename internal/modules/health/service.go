package health

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

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
	StateDir          string
	Folder            string
	Provider          string
	ProviderProbeKind provider.ProviderProbeKind
	WriteBackProvider string
	Deep              bool
	Session           provider.Session
	Reader            provider.Reader
	WriteBack         provider.HealthProber
	WriteBacks        []WriteBackProbe
	LookPath          func(string) (string, error)
	Getenv            func(string) string
}

type WriteBackProbe struct {
	Name   string
	Driver string
	Health provider.HealthProber
}

func (s *Service) Run(ctx context.Context) (Result, error) {
	if s == nil {
		return Result{}, fmt.Errorf("health service 不能为空")
	}
	if s.Session == nil {
		return Result{}, fmt.Errorf("health session 不能为空")
	}
	if strings.TrimSpace(s.StateDir) == "" {
		return Result{}, fmt.Errorf("health state dir 不能为空")
	}
	if s.Deep && s.Reader == nil {
		return Result{}, fmt.Errorf("deep health reader 不能为空")
	}

	checks := []Check{
		s.checkStateDir(),
		s.checkGPG(),
		s.checkCachedToken(),
	}
	if s.Deep {
		checks = append(checks,
			s.checkStateDirWritable(),
			s.checkProvider(ctx),
		)
		checks = append(checks, s.checkWriteBacks(ctx)...)
	}
	return Result{Checks: checks}, nil
}

func (s *Service) checkStateDir() Check {
	stateDir := strings.TrimSpace(s.StateDir)
	info, err := os.Stat(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Check{Name: "state_dir", Detail: "不存在"}
		}
		return Check{Name: "state_dir", Detail: fmt.Sprintf("不可访问: %v", err)}
	}
	if !info.IsDir() {
		return Check{Name: "state_dir", Detail: "目录类型无效"}
	}
	return Check{Name: "state_dir", OK: true, Detail: stateDir}
}

func (s *Service) checkStateDirWritable() Check {
	stateDir := strings.TrimSpace(s.StateDir)
	file, err := os.CreateTemp(stateDir, "health-*.tmp")
	if err != nil {
		return Check{Name: "state_dir_write", Detail: fmt.Sprintf("不可写: %v", err)}
	}
	path := file.Name()
	_ = file.Close()
	_ = os.Remove(path)
	return Check{Name: "state_dir_write", OK: true, Detail: stateDir}
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
		if time.Until(token.ExpiresAt) <= 0 {
			if strings.TrimSpace(token.RefreshToken) == "" {
				return Check{Name: "cached_token", Detail: detail + "，且无 refresh token"}
			}
			detail += "，已过期，可执行刷新"
		}
	}
	return Check{Name: "cached_token", OK: true, Detail: detail}
}

func (s *Service) checkProvider(ctx context.Context) Check {
	switch s.providerProbeKind() {
	case provider.ProviderProbeFolderList:
		folder := strings.TrimSpace(s.Folder)
		if folder == "" {
			folder = "INBOX"
		}
		_, err := s.Reader.LatestMessagesInFolder(ctx, folder, 0, 1)
		if err != nil {
			return Check{Name: "provider_probe", Detail: err.Error()}
		}
		return Check{Name: "provider_probe", OK: true, Detail: "imap folder=" + folder}
	case provider.ProviderProbeIdentity:
		user, err := s.Reader.Me(ctx)
		if err != nil {
			return Check{Name: "provider_probe", Detail: err.Error()}
		}
		return Check{Name: "provider_probe", OK: true, Detail: user.Account()}
	default:
		return Check{Name: "provider_probe", Detail: "未声明 provider 探测策略"}
	}
}

func (s *Service) providerProbeKind() provider.ProviderProbeKind {
	if s != nil && s.ProviderProbeKind != "" {
		return s.ProviderProbeKind
	}
	if sourceSpec, ok := provider.LookupSourceSpec(s.Provider); ok && sourceSpec.ProbeKind != "" {
		return sourceSpec.ProbeKind
	}
	return provider.ProviderProbeIdentity
}

func (s *Service) checkWriteBack(ctx context.Context) Check {
	if s.WriteBack == nil {
		return Check{Name: "writeback_probe", Detail: "该回写实现未提供健康探测能力"}
	}
	detail, err := s.WriteBack.HealthCheck(ctx)
	if err != nil {
		return Check{Name: "writeback_probe", Detail: err.Error()}
	}
	if strings.TrimSpace(detail) == "" {
		detail = normalizedWriteBackProvider(s.Provider, s.WriteBackProvider)
	}
	return Check{Name: "writeback_probe", OK: true, Detail: detail}
}

func (s *Service) checkWriteBacks(ctx context.Context) []Check {
	if len(s.WriteBacks) == 0 {
		if s.WriteBack == nil {
			return nil
		}
		return []Check{s.checkWriteBack(ctx)}
	}

	checks := make([]Check, 0, len(s.WriteBacks))
	for _, probe := range s.WriteBacks {
		checks = append(checks, s.checkNamedWriteBack(ctx, probe))
	}
	return checks
}

func (s *Service) checkNamedWriteBack(ctx context.Context, probe WriteBackProbe) Check {
	name := strings.TrimSpace(probe.Name)
	if name == "" {
		name = normalizedWriteBackProvider("", probe.Driver)
	}

	if probe.Health == nil {
		return Check{Name: "writeback_probe[" + name + "]", Detail: "该回写实现未提供健康探测能力"}
	}
	detail, err := probe.Health.HealthCheck(ctx)
	if err != nil {
		return Check{Name: "writeback_probe[" + name + "]", Detail: err.Error()}
	}
	if strings.TrimSpace(detail) == "" {
		detail = normalizedWriteBackProvider("", probe.Driver)
	}
	return Check{Name: "writeback_probe[" + name + "]", OK: true, Detail: detail}
}

func normalizedWriteBackProvider(providerName, writeBackProvider string) string {
	if value := strings.ToLower(strings.TrimSpace(writeBackProvider)); value != "" {
		return value
	}
	if value := strings.ToLower(strings.TrimSpace(providerName)); value != "" {
		return value
	}
	return "unknown"
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
