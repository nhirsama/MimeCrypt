package appconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

const (
	defaultClientID         = "fff3108f-14f7-4877-9739-1a2766e5ca9a"
	defaultTenant           = "organizations"
	defaultAuthorityBaseURL = "https://login.microsoftonline.com"
	defaultGraphBaseURL     = "https://graph.microsoft.com/v1.0"
	defaultGraphScopes      = "https://graph.microsoft.com/Mail.ReadWrite https://graph.microsoft.com/User.Read offline_access openid profile"
	defaultEWSBaseURL       = "https://outlook.office365.com/EWS/Exchange.asmx"
	defaultEWSScopes        = "https://outlook.office365.com/EWS.AccessAsUser.All"
	defaultIMAPAddr         = "outlook.office365.com:993"
	defaultIMAPScopes       = "https://outlook.office.com/IMAP.AccessAsUser.All offline_access"
	defaultOutputDir        = "output"
	defaultTokenFileName    = "token.json"
	defaultTokenStore       = "file"
	defaultKeyringService   = "mimecrypt"
)

type Config struct {
	TopologyPath string
	Auth         AuthConfig
	Mail         MailConfig
}

type AuthConfig struct {
	ClientID         string
	Tenant           string
	AuthorityBaseURL string
	GraphScopes      []string
	EWSScopes        []string
	IMAPScopes       []string
	StateDir         string
	TokenStore       string
	KeyringService   string
}

type MailConfig struct {
	Client   MailClientConfig
	Pipeline MailPipelineConfig
	Sync     MailSyncConfig
}

type MailClientConfig struct {
	GraphBaseURL string
	EWSBaseURL   string
	IMAPAddr     string
	IMAPUsername string
}

type MailPipelineConfig struct {
	OutputDir      string
	WorkDir        string
	ProtectSubject bool
	BackupDir      string
	BackupKeyID    string
	AuditLogPath   string
	AuditStdout    bool
}

type MailSyncConfig struct {
	StateDir string
}

// LoadFromEnv 从环境变量加载 CLI 所需配置。
func LoadFromEnv() (Config, error) {
	stateDir, err := defaultStateDir()
	if err != nil {
		return Config{}, err
	}
	stateDir = getenvDefault("MIMECRYPT_STATE_DIR", stateDir)
	protectSubject, err := getenvBoolDefault("MIMECRYPT_PROTECT_SUBJECT", false)
	if err != nil {
		return Config{}, fmt.Errorf("解析 MIMECRYPT_PROTECT_SUBJECT 失败: %w", err)
	}
	auditStdout, err := getenvBoolDefault("MIMECRYPT_AUDIT_STDOUT", false)
	if err != nil {
		return Config{}, fmt.Errorf("解析 MIMECRYPT_AUDIT_STDOUT 失败: %w", err)
	}
	imapUsername := strings.TrimSpace(os.Getenv("MIMECRYPT_IMAP_USERNAME"))

	return Config{
		TopologyPath: getenvDefault("MIMECRYPT_TOPOLOGY_PATH", DefaultTopologyPath(stateDir)),
		Auth: AuthConfig{
			ClientID:         getenvDefault("MIMECRYPT_CLIENT_ID", defaultClientID),
			Tenant:           getenvDefault("MIMECRYPT_TENANT", defaultTenant),
			AuthorityBaseURL: getenvDefault("MIMECRYPT_AUTHORITY_BASE_URL", defaultAuthorityBaseURL),
			GraphScopes:      splitScopes(getenvDefault("MIMECRYPT_GRAPH_SCOPES", defaultGraphScopes)),
			EWSScopes:        splitScopes(getenvDefault("MIMECRYPT_EWS_SCOPES", defaultEWSScopes)),
			IMAPScopes:       splitScopes(getenvDefault("MIMECRYPT_IMAP_SCOPES", defaultIMAPScopes)),
			StateDir:         stateDir,
			TokenStore:       getenvDefault("MIMECRYPT_TOKEN_STORE", defaultTokenStore),
			KeyringService:   getenvDefault("MIMECRYPT_KEYRING_SERVICE", defaultKeyringService),
		},
		Mail: MailConfig{
			Client: MailClientConfig{
				GraphBaseURL: getenvDefault("MIMECRYPT_GRAPH_BASE_URL", defaultGraphBaseURL),
				EWSBaseURL:   getenvDefault("MIMECRYPT_EWS_BASE_URL", defaultEWSBaseURL),
				IMAPAddr:     getenvDefault("MIMECRYPT_IMAP_ADDR", defaultIMAPAddr),
				IMAPUsername: imapUsername,
			},
			Pipeline: MailPipelineConfig{
				OutputDir:      getenvDefault("MIMECRYPT_OUTPUT_DIR", defaultOutputDir),
				WorkDir:        os.Getenv("MIMECRYPT_WORK_DIR"),
				ProtectSubject: protectSubject,
				BackupDir:      getenvDefault("MIMECRYPT_BACKUP_DIR", "backup"),
				BackupKeyID:    os.Getenv("MIMECRYPT_BACKUP_KEY_ID"),
				AuditLogPath:   getenvDefault("MIMECRYPT_AUDIT_LOG_PATH", DefaultAuditLogPath(stateDir)),
				AuditStdout:    auditStdout,
			},
			Sync: MailSyncConfig{
				StateDir: stateDir,
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
	if strings.TrimSpace(c.StateDir) == "" {
		return fmt.Errorf("state dir 不能为空")
	}
	switch c.TokenStoreMode() {
	case "file":
	case "keyring":
		if strings.TrimSpace(c.KeyringServiceName()) == "" {
			return fmt.Errorf("keyring service 不能为空")
		}
	default:
		return fmt.Errorf("token store 不支持: %s", c.TokenStore)
	}
	if len(c.GraphScopes) == 0 && len(c.EWSScopes) == 0 && len(c.IMAPScopes) == 0 {
		return fmt.Errorf("至少需要一组 protocol scopes")
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

func (c MailClientConfig) ValidateIMAP() error {
	if strings.TrimSpace(c.IMAPAddr) == "" {
		return fmt.Errorf("imap addr 不能为空")
	}
	if strings.TrimSpace(c.IMAPUsername) == "" {
		return fmt.Errorf("imap username 不能为空")
	}

	return nil
}

func (c MailConfig) ValidatePipelineBase() error {
	if strings.TrimSpace(c.Sync.StateDir) == "" {
		return fmt.Errorf("state dir 不能为空")
	}
	if strings.TrimSpace(c.Pipeline.BackupDir) == "" {
		return fmt.Errorf("backup dir 不能为空")
	}
	if !c.Pipeline.HasAuditOutput() {
		return fmt.Errorf("至少需要一个审计输出：audit log path 或 audit stdout")
	}
	return nil
}

func (c MailPipelineConfig) HasAuditOutput() bool {
	return strings.TrimSpace(c.AuditLogPath) != "" || c.AuditStdout
}

func (c AuthConfig) TokenPath() string {
	return filepath.Join(c.StateDir, defaultTokenFileName)
}

func (c AuthConfig) TokenStoreMode() string {
	value := strings.ToLower(strings.TrimSpace(c.TokenStore))
	if value == "" {
		return defaultTokenStore
	}
	return value
}

func (c AuthConfig) KeyringServiceName() string {
	value := strings.TrimSpace(c.KeyringService)
	if value == "" {
		return defaultKeyringService
	}
	return value
}

func (c MailConfig) FlowProducerStatePathFor(sourceName, driver, folder string) string {
	scope := flowStateScope("", sourceName, driver, folder)
	return filepath.Join(c.Sync.StateDir, "flow-sync-"+sanitizeFileComponent(scope)+".json")
}

func (c MailConfig) FlowStateDirFor(routeName, sourceName, driver, folder string) string {
	scope := flowStateScope(routeName, sourceName, driver, folder)
	return filepath.Join(c.Sync.StateDir, "flow-state", sanitizeFileComponent(scope))
}

func (c Config) RunLockPathFor(sourceName, driver, folder string) string {
	scope := flowStateScope("", sourceName, driver, folder)
	return filepath.Join(c.Auth.StateDir, "run-"+sanitizeFileComponent(scope)+".lock")
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

func flowStateScope(routeName, sourceName, driver, folder string) string {
	parts := make([]string, 0, 4)
	if value := strings.TrimSpace(routeName); value != "" {
		parts = append(parts, value)
	}
	if value := strings.TrimSpace(sourceName); value != "" {
		parts = append(parts, value)
	}
	if value := strings.TrimSpace(driver); value != "" {
		parts = append(parts, value)
	}
	if value := strings.TrimSpace(folder); value != "" {
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, "-")
}
