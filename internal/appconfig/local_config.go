package appconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type LocalConfig struct {
	IMAPUsername string `json:"imapUsername,omitempty"`
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
	cfg.IMAPUsername = strings.TrimSpace(cfg.IMAPUsername)
	return cfg, nil
}

func SaveLocalConfig(stateDir string, cfg LocalConfig) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("state dir 不能为空")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("创建本地配置目录失败: %w", err)
	}

	cfg.IMAPUsername = strings.TrimSpace(cfg.IMAPUsername)
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
