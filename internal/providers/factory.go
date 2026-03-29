package providers

import (
	"context"
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
	clients, err := BuildSourceClients(cfg)
	if err != nil {
		return provider.Clients{}, err
	}
	writer, err := BuildWriteBackWriterWithSession(cfg, clients.Session)
	if err != nil {
		return provider.Clients{}, err
	}
	clients.Writer = writer
	return clients, nil
}

func BuildSourceClients(cfg appconfig.Config) (provider.Clients, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "graph":
		return graph.Build(cfg)
	case "imap":
		return imap.Build(cfg)
	default:
		return provider.Clients{}, fmt.Errorf("不支持的邮件服务提供方: %s", cfg.Provider)
	}
}

func BuildWriteBackWriter(cfg appconfig.Config) (provider.Writer, error) {
	return BuildWriteBackWriterWithSession(cfg, nil)
}

type scopedAccessSession interface {
	provider.Session
	AccessTokenForScopes(ctx context.Context, scopes []string) (string, error)
}

func BuildWriteBackWriterWithSession(cfg appconfig.Config, session provider.Session) (provider.Writer, error) {
	providerName := normalizedWriteBackProvider(cfg.Provider, cfg.Mail.Pipeline.WriteBackProvider)
	if session == nil {
		var err error
		session, err = auth.NewSession(cfg.Auth, nil)
		if err != nil {
			return nil, err
		}
	}

	switch providerName {
	case "graph":
		return graph.NewWriter(cfg, session)
	case "ews":
		scoped, ok := session.(scopedAccessSession)
		if !ok {
			return nil, fmt.Errorf("当前 session 不支持按 scopes 获取 token")
		}
		return graph.NewEWSWriter(cfg, scoped)
	case "imap":
		scoped, ok := session.(scopedAccessSession)
		if !ok {
			return nil, fmt.Errorf("当前 session 不支持按 scopes 获取 token")
		}
		return imap.NewWriter(cfg, scoped)
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
