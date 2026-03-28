package appconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	defaultProvider          = "graph"
	defaultClientID          = "fff3108f-14f7-4877-9739-1a2766e5ca9a"
	defaultTenant            = "organizations"
	defaultAuthorityBaseURL  = "https://login.microsoftonline.com"
	defaultGraphBaseURL      = "https://graph.microsoft.com/v1.0"
	defaultGraphScopes       = "https://graph.microsoft.com/Mail.ReadWrite https://graph.microsoft.com/User.Read offline_access openid profile"
	defaultEWSBaseURL        = "https://outlook.office365.com/EWS/Exchange.asmx"
	defaultEWSScopes         = "https://outlook.office365.com/EWS.AccessAsUser.All"
	defaultWriteBackProvider = "ews"
	defaultFolder            = "inbox"
	defaultPollInterval      = time.Minute
	defaultCycleTimeout      = 2 * time.Minute
	defaultOutputDir         = "output"
)

type Config struct {
	Provider string
	Auth     AuthConfig
	Mail     MailConfig
}

type AuthConfig struct {
	ClientID         string
	Tenant           string
	AuthorityBaseURL string
	GraphScopes      []string
	EWSScopes        []string
	StateDir         string
}

type MailConfig struct {
	Client   MailClientConfig
	Pipeline MailPipelineConfig
	Sync     MailSyncConfig
}

type MailClientConfig struct {
	GraphBaseURL string
	EWSBaseURL   string
}

type MailPipelineConfig struct {
	OutputDir         string
	SaveOutput        bool
	BackupDir         string
	BackupKeyID       string
	AuditLogPath      string
	WriteBackProvider string
	WriteBackFolder   string
}

type MailSyncConfig struct {
	Folder       string
	StateDir     string
	PollInterval time.Duration
	CycleTimeout time.Duration
}

// LoadFromEnv 从环境变量加载 CLI 所需配置。
func LoadFromEnv() (Config, error) {
	stateDir, err := defaultStateDir()
	if err != nil {
		return Config{}, err
	}
	saveOutput, err := getenvBoolDefault("MIMECRYPT_SAVE_OUTPUT", false)
	if err != nil {
		return Config{}, fmt.Errorf("解析 MIMECRYPT_SAVE_OUTPUT 失败: %w", err)
	}

	return Config{
		Provider: getenvDefault("MIMECRYPT_PROVIDER", defaultProvider),
		Auth: AuthConfig{
			ClientID:         getenvDefault("MIMECRYPT_CLIENT_ID", defaultClientID),
			Tenant:           getenvDefault("MIMECRYPT_TENANT", defaultTenant),
			AuthorityBaseURL: getenvDefault("MIMECRYPT_AUTHORITY_BASE_URL", defaultAuthorityBaseURL),
			GraphScopes:      splitScopes(getenvDefault("MIMECRYPT_GRAPH_SCOPES", defaultGraphScopes)),
			EWSScopes:        splitScopes(getenvDefault("MIMECRYPT_EWS_SCOPES", defaultEWSScopes)),
			StateDir:         getenvDefault("MIMECRYPT_STATE_DIR", stateDir),
		},
		Mail: MailConfig{
			Client: MailClientConfig{
				GraphBaseURL: getenvDefault("MIMECRYPT_GRAPH_BASE_URL", defaultGraphBaseURL),
				EWSBaseURL:   getenvDefault("MIMECRYPT_EWS_BASE_URL", defaultEWSBaseURL),
			},
			Pipeline: MailPipelineConfig{
				OutputDir:         getenvDefault("MIMECRYPT_OUTPUT_DIR", defaultOutputDir),
				SaveOutput:        saveOutput,
				BackupDir:         getenvDefault("MIMECRYPT_BACKUP_DIR", "backup"),
				BackupKeyID:       os.Getenv("MIMECRYPT_BACKUP_KEY_ID"),
				AuditLogPath:      getenvDefault("MIMECRYPT_AUDIT_LOG_PATH", DefaultAuditLogPath(stateDir)),
				WriteBackProvider: getenvDefault("MIMECRYPT_WRITEBACK_PROVIDER", defaultWriteBackProvider),
				WriteBackFolder:   os.Getenv("MIMECRYPT_WRITEBACK_FOLDER"),
			},
			Sync: MailSyncConfig{
				Folder:       getenvDefault("MIMECRYPT_FOLDER", defaultFolder),
				StateDir:     getenvDefault("MIMECRYPT_STATE_DIR", stateDir),
				PollInterval: defaultPollInterval,
				CycleTimeout: defaultCycleTimeout,
			},
		},
	}, nil
}

// Validate 校验登录和刷新 token 所需配置。
func (c AuthConfig) Validate() error {
	var missing []string

	if strings.TrimSpace(c.ClientID) == "" {
		missing = append(missing, "MIMECRYPT_CLIENT_ID")
	}

	if len(missing) > 0 {
		return fmt.Errorf("缺少必需配置: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(c.Tenant) == "" {
		return fmt.Errorf("tenant 不能为空")
	}
	if strings.TrimSpace(c.AuthorityBaseURL) == "" {
		return fmt.Errorf("authority base URL 不能为空")
	}
	if len(c.GraphScopes) == 0 {
		return fmt.Errorf("graph scopes 不能为空")
	}
	if strings.TrimSpace(c.StateDir) == "" {
		return fmt.Errorf("state dir 不能为空")
	}

	return nil
}

// ValidateClient 校验 Graph 邮件客户端所需配置。
func (c MailConfig) ValidateClient() error {
	return c.Client.Validate()
}

// Validate 校验 Graph 邮件客户端所需配置。
func (c MailClientConfig) Validate() error {
	if strings.TrimSpace(c.GraphBaseURL) == "" {
		return fmt.Errorf("graph base URL 不能为空")
	}

	return nil
}

func (c MailClientConfig) ValidateEWS() error {
	if strings.TrimSpace(c.EWSBaseURL) == "" {
		return fmt.Errorf("ews base URL 不能为空")
	}

	return nil
}

// ValidateSync 校验邮件同步所需配置。
func (c MailConfig) ValidateSync() error {
	if err := c.ValidateClient(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Sync.StateDir) == "" {
		return fmt.Errorf("state dir 不能为空")
	}
	if c.Pipeline.SaveOutput && strings.TrimSpace(c.Pipeline.OutputDir) == "" {
		return fmt.Errorf("output dir 不能为空")
	}
	if strings.TrimSpace(c.Pipeline.BackupDir) == "" {
		return fmt.Errorf("backup dir 不能为空")
	}
	if strings.TrimSpace(c.Pipeline.AuditLogPath) == "" {
		return fmt.Errorf("audit log path 不能为空")
	}
	switch strings.ToLower(strings.TrimSpace(c.Pipeline.WriteBackProvider)) {
	case "", "graph":
	case "ews":
		if err := c.Client.ValidateEWS(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("write back provider 不支持: %s", c.Pipeline.WriteBackProvider)
	}
	if strings.TrimSpace(c.Sync.Folder) == "" {
		return fmt.Errorf("folder 不能为空")
	}
	if c.Sync.PollInterval <= 0 {
		return fmt.Errorf("poll interval 必须大于 0")
	}
	if c.Sync.CycleTimeout <= 0 {
		return fmt.Errorf("cycle timeout 必须大于 0")
	}

	return nil
}

func (c AuthConfig) TokenPath() string {
	return filepath.Join(c.StateDir, "graph-token.json")
}

func (c MailConfig) SyncStatePath() string {
	return filepath.Join(c.Sync.StateDir, "sync-"+sanitizeFileComponent(c.Sync.Folder)+".json")
}

func DefaultAuditLogPath(stateDir string) string {
	return filepath.Join(stateDir, "audit.jsonl")
}

func defaultStateDir() (string, error) {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("获取用户配置目录失败: %w", err)
	}

	return filepath.Join(userConfigDir, "mimecrypt"), nil
}

func getenvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func getenvBoolDefault(key string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, err
	}
	return parsed, nil
}

func splitScopes(value string) []string {
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return nil
	}

	return parts
}

func sanitizeFileComponent(value string) string {
	if value == "" {
		return "unknown"
	}

	var builder strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(r)
		case r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}

	result := strings.Trim(builder.String(), "._")
	if result == "" {
		return "unknown"
	}

	return result
}
