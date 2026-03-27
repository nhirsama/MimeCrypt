package providers

import (
	"fmt"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers/graph"
)

// Build 根据配置构造当前使用的邮件服务提供方实现。
func Build(cfg appconfig.Config) (provider.Session, provider.Reader, provider.Writer, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "graph":
		return graph.Build(cfg)
	default:
		return nil, nil, nil, fmt.Errorf("不支持的邮件服务提供方: %s", cfg.Provider)
	}
}
