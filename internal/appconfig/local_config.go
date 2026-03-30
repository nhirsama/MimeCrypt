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
	Drivers      []string              `json:"drivers,omitempty"`
	LoginConfig  string                `json:"loginConfig,omitempty"`
	IMAPUsername string                `json:"imapUsername,omitempty"`
	Microsoft    *MicrosoftLocalConfig `json:"microsoft,omitempty"`
}

type MicrosoftLocalConfig struct {
	ClientID         string `json:"clientId,omitempty"`
	Tenant           string `json:"tenant,omitempty"`
	AuthorityBaseURL string `json:"authorityBaseURL,omitempty"`
	IMAPUsername     string `json:"imapUsername,omitempty"`
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
	cfg.LoginConfig = strings.TrimSpace(cfg.LoginConfig)
	cfg.IMAPUsername = strings.TrimSpace(cfg.IMAPUsername)

	seen := make(map[string]struct{}, len(cfg.Drivers))
	drivers := make([]string, 0, len(cfg.Drivers))
	for _, driver := range cfg.Drivers {
		driver = strings.ToLower(strings.TrimSpace(driver))
		if driver == "" {
			continue
		}
		if _, ok := seen[driver]; ok {
			continue
		}
		seen[driver] = struct{}{}
		drivers = append(drivers, driver)
	}
	sort.Strings(drivers)
	cfg.Drivers = drivers

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
