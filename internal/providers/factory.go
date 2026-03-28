package providers

import (
	"fmt"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers/graph"
	"mimecrypt/internal/providers/imap"
)

// Build 根据配置构造当前使用的邮件服务提供方实现。
func Build(cfg appconfig.Config) (provider.Clients, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "graph":
		return graph.Build(cfg)
	case "imap":
		return imap.Build(cfg)
	default:
		return provider.Clients{}, fmt.Errorf("不支持的邮件服务提供方: %s", cfg.Provider)
	}
}
