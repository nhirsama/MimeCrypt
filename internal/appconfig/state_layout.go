package appconfig

import (
	"path/filepath"
	"strings"
)

// StateLayout 表示某一套认证、同步与审计文件默认共享的状态根目录。
type StateLayout struct {
	RootDir string
}

func NewStateLayout(rootDir string) StateLayout {
	return StateLayout{RootDir: strings.TrimSpace(rootDir)}
}

func (l StateLayout) Root() string {
	return strings.TrimSpace(l.RootDir)
}

func (l StateLayout) AuditLogPath() string {
	root := l.Root()
	if root == "" {
		return ""
	}
	return DefaultAuditLogPath(root)
}

func (c Config) StateLayout() StateLayout {
	if stateDir := strings.TrimSpace(c.Mail.Sync.StateDir); stateDir != "" {
		return NewStateLayout(stateDir)
	}
	return NewStateLayout(c.Auth.StateDir)
}

func (c Config) WithStateLayout(layout StateLayout) Config {
	return c.WithStateDir(layout.Root())
}

func (c Config) WithStateDir(stateDir string) Config {
	cfg := c
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return cfg
	}

	previousLayout := cfg.StateLayout()
	previousAuditLogPath := strings.TrimSpace(cfg.Mail.Pipeline.AuditLogPath)

	cfg.Auth.StateDir = stateDir
	cfg.Mail.Sync.StateDir = stateDir

	if previousAuditLogPath == "" || previousAuditLogPath == previousLayout.AuditLogPath() {
		cfg.Mail.Pipeline.AuditLogPath = DefaultAuditLogPath(stateDir)
	} else if !filepath.IsAbs(previousAuditLogPath) && previousAuditLogPath == filepath.Base(previousLayout.AuditLogPath()) {
		cfg.Mail.Pipeline.AuditLogPath = filepath.Join(stateDir, previousAuditLogPath)
	}

	return cfg
}
