package appconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type LocalConfig struct {
	RuntimeName  string                `json:"runtime,omitempty"`
	AuthProfile  string                `json:"authProfile,omitempty"`
	IMAPUsername string                `json:"imapUsername,omitempty"`
	Microsoft    *MicrosoftLocalConfig `json:"microsoft,omitempty"`

	// Drivers/LoginConfig 仅保留内存兼容，便于 provider runtime 迁移期继续读取。
	Drivers     []string `json:"-"`
	LoginConfig string   `json:"-"`
}

type MicrosoftLocalConfig struct {
	ClientID         string `json:"clientId,omitempty"`
	Tenant           string `json:"tenant,omitempty"`
	AuthorityBaseURL string `json:"authorityBaseURL,omitempty"`
	IMAPUsername     string `json:"imapUsername,omitempty"`
}

func (c *LocalConfig) UnmarshalJSON(data []byte) error {
	type localConfigJSON struct {
		RuntimeName  string                `json:"runtime,omitempty"`
		AuthProfile  string                `json:"authProfile,omitempty"`
		Drivers      []string              `json:"drivers,omitempty"`
		LoginConfig  string                `json:"loginConfig,omitempty"`
		IMAPUsername string                `json:"imapUsername,omitempty"`
		Microsoft    *MicrosoftLocalConfig `json:"microsoft,omitempty"`
	}

	var decoded localConfigJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	*c = LocalConfig{
		RuntimeName:  decoded.RuntimeName,
		AuthProfile:  decoded.AuthProfile,
		Drivers:      append([]string(nil), decoded.Drivers...),
		LoginConfig:  decoded.LoginConfig,
		IMAPUsername: decoded.IMAPUsername,
		Microsoft:    decoded.Microsoft,
	}
	return nil
}

func LoadLocalConfig(stateDir string) (LocalConfig, error) {
	path := LocalConfigPath(stateDir)
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return LocalConfig{}, nil
	}
	if err != nil {
		return LocalConfig{}, fmt.Errorf("读取本地配置失败: %w", err)
	}

	var cfg LocalConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return LocalConfig{}, fmt.Errorf("解析本地配置失败: %w", err)
	}
	return normalizeLocalConfig(cfg), nil
}

func SaveLocalConfig(stateDir string, cfg LocalConfig) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("state dir 不能为空")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("创建本地配置目录失败: %w", err)
	}

	cfg = normalizeLocalConfig(cfg)
	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化本地配置失败: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(LocalConfigPath(stateDir), content, 0o600); err != nil {
		return fmt.Errorf("写入本地配置失败: %w", err)
	}
	return nil
}

func ClearLocalConfig(stateDir string) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("state dir 不能为空")
	}
	if err := os.Remove(LocalConfigPath(stateDir)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("删除本地配置失败: %w", err)
	}
	return nil
}

func LocalConfigPath(stateDir string) string {
	return filepath.Join(stateDir, "config.json")
}

func normalizeLocalConfig(cfg LocalConfig) LocalConfig {
	cfg.RuntimeName = strings.TrimSpace(firstNonEmptyString(cfg.RuntimeName, cfg.LoginConfig))
	cfg.LoginConfig = cfg.RuntimeName
	cfg.AuthProfile = CredentialAuthProfileForHints(firstNonEmptyStrings(CredentialAuthHintsForProfile(cfg.AuthProfile), cfg.Drivers))
	cfg.IMAPUsername = strings.TrimSpace(cfg.IMAPUsername)
	cfg.Drivers = CredentialAuthHintsForProfile(cfg.AuthProfile)

	if cfg.Microsoft != nil {
		cfg.Microsoft.ClientID = strings.TrimSpace(cfg.Microsoft.ClientID)
		cfg.Microsoft.Tenant = strings.TrimSpace(cfg.Microsoft.Tenant)
		cfg.Microsoft.AuthorityBaseURL = strings.TrimSpace(cfg.Microsoft.AuthorityBaseURL)
		cfg.Microsoft.IMAPUsername = strings.TrimSpace(cfg.Microsoft.IMAPUsername)
		if cfg.IMAPUsername == "" {
			cfg.IMAPUsername = cfg.Microsoft.IMAPUsername
		}
		if cfg.Microsoft.ClientID == "" &&
			cfg.Microsoft.Tenant == "" &&
			cfg.Microsoft.AuthorityBaseURL == "" &&
			cfg.Microsoft.IMAPUsername == "" {
			cfg.Microsoft = nil
		}
	}

	return cfg
}

func (c LocalConfig) Normalize() LocalConfig {
	return normalizeLocalConfig(c)
}

func (c LocalConfig) EffectiveRuntimeName() string {
	return strings.TrimSpace(normalizeLocalConfig(c).RuntimeName)
}

func (c LocalConfig) EffectiveAuthProfile() string {
	return strings.TrimSpace(normalizeLocalConfig(c).AuthProfile)
}

func (c LocalConfig) AuthHintNames() []string {
	return append([]string(nil), normalizeLocalConfig(c).Drivers...)
}

func (c LocalConfig) WithRuntimeName(runtimeName string) LocalConfig {
	c.RuntimeName = strings.TrimSpace(runtimeName)
	c.LoginConfig = c.RuntimeName
	return normalizeLocalConfig(c)
}

func (c LocalConfig) WithAuthHintNames(hints []string) LocalConfig {
	c.AuthProfile = CredentialAuthProfileForHints(hints)
	c.Drivers = append([]string(nil), hints...)
	return normalizeLocalConfig(c)
}

func CredentialAuthProfileForHints(hints []string) string {
	normalized := normalizeCredentialAuthHints(hints)
	if len(normalized) == 0 {
		return ""
	}
	return strings.Join(normalized, "+")
}

func CredentialAuthHintsForProfile(profile string) []string {
	if strings.TrimSpace(profile) == "" {
		return nil
	}
	tokens := strings.FieldsFunc(profile, func(r rune) bool {
		return r == '+' || r == ',' || r == '/' || r == '|' || r == ';' || r == ':'
	})
	return normalizeCredentialAuthHints(tokens)
}

func normalizeCredentialAuthHints(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	known := make([]string, 0, len(values))
	unknown := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		if credentialAuthHintRank(value) >= 0 {
			known = append(known, value)
			continue
		}
		unknown = append(unknown, value)
	}
	sort.Slice(known, func(i, j int) bool {
		return credentialAuthHintRank(known[i]) < credentialAuthHintRank(known[j])
	})
	sort.Strings(unknown)
	return append(known, unknown...)
}

func credentialAuthHintRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "graph":
		return 0
	case "ews":
		return 1
	case "imap":
		return 2
	default:
		return -1
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyStrings(values ...[]string) []string {
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		return append([]string(nil), value...)
	}
	return nil
}
