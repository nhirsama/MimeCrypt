package providers

import (
	"fmt"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers/graph"
	"mimecrypt/internal/providers/imap"
)

// Build 根据配置构造当前使用的邮件服务提供方实现。
func Build(cfg appconfig.Config) (provider.Clients, error) {
	clients, err := buildSourceClients(cfg)
	if err != nil {
		return provider.Clients{}, err
	}
	writer, err := buildWriteBackWriter(cfg)
	if err != nil {
		return provider.Clients{}, err
	}
	clients.Writer = writer
	return clients, nil
}

func buildSourceClients(cfg appconfig.Config) (provider.Clients, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "graph":
		return graph.Build(cfg)
	case "imap":
		return imap.Build(cfg)
	default:
		return provider.Clients{}, fmt.Errorf("不支持的邮件服务提供方: %s", cfg.Provider)
	}
}

func buildWriteBackWriter(cfg appconfig.Config) (provider.Writer, error) {
	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		return nil, err
	}

	switch normalizedWriteBackProvider(cfg.Provider, cfg.Mail.Pipeline.WriteBackProvider) {
	case "graph":
		return graph.NewWriter(cfg, session)
	case "ews":
		return graph.NewEWSWriter(cfg, session)
	case "imap":
		return imap.NewWriter(cfg, session)
	default:
		return nil, fmt.Errorf("不支持的回写后端: %s", cfg.Mail.Pipeline.WriteBackProvider)
	}
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
