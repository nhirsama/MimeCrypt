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

func BuildSourceClients(cfg appconfig.Config) (provider.SourceClients, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "graph":
		return graph.BuildSourceClients(cfg)
	case "imap":
		return imap.BuildSourceClients(cfg)
	default:
		return provider.SourceClients{}, fmt.Errorf("不支持的邮件服务提供方: %s", cfg.Provider)
	}
}

func BuildSourceClientsWithSession(cfg appconfig.Config, session provider.Session) (provider.SourceClients, error) {
	switch normalizedSourceProvider(cfg.Provider) {
	case "graph":
		return graph.BuildSourceClientsWithSession(cfg, session)
	case "imap":
		return imap.BuildSourceClientsWithSession(cfg, session)
	default:
		return provider.SourceClients{}, fmt.Errorf("不支持的邮件服务提供方: %s", cfg.Provider)
	}
}

func BuildWriteBackClients(cfg appconfig.Config) (provider.SinkClients, error) {
	return BuildWriteBackClientsWithSession(cfg, nil)
}

func BuildWriteBackClientsWithSession(cfg appconfig.Config, session provider.Session) (provider.SinkClients, error) {
	providerName := normalizedWriteBackProvider(cfg.Provider, cfg.Mail.Pipeline.WriteBackProvider)
	if session == nil {
		var err error
		session, err = auth.NewSession(sessionAuthConfig(cfg), nil)
		if err != nil {
			return provider.SinkClients{}, err
		}
	}

	switch providerName {
	case "graph":
		return graph.NewWriterClients(cfg, session)
	case "ews":
		return graph.NewEWSWriterClients(cfg, session)
	case "imap":
		return imap.NewWriterClients(cfg, session)
	default:
		return provider.SinkClients{}, fmt.Errorf("不支持的回写后端: %s", cfg.Mail.Pipeline.WriteBackProvider)
	}
}

func sessionAuthConfig(cfg appconfig.Config) appconfig.AuthConfig {
	authCfg := cfg.Auth
	sourceProvider := normalizedSourceProvider(cfg.Provider)
	writeBackProvider := normalizedWriteBackProvider(cfg.Provider, cfg.Mail.Pipeline.WriteBackProvider)

	needsGraph := sourceProvider == "graph" || writeBackProvider == "graph" || writeBackProvider == "ews"
	needsEWS := writeBackProvider == "ews"
	needsIMAP := sourceProvider == "imap" || writeBackProvider == "imap"

	if !needsGraph {
		authCfg.GraphScopes = nil
	}
	if !needsEWS {
		authCfg.EWSScopes = nil
	}
	if !needsIMAP {
		authCfg.IMAPScopes = nil
	}
	return authCfg
}

func normalizedSourceProvider(providerName string) string {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "", "graph":
		return "graph"
	case "imap":
		return "imap"
	default:
		return strings.ToLower(strings.TrimSpace(providerName))
	}
}

func normalizedWriteBackProvider(providerName, writeBackProvider string) string {
	if value := strings.ToLower(strings.TrimSpace(writeBackProvider)); value != "" {
		return value
	}
	if value := normalizedSourceProvider(providerName); value != "" {
		return value
	}
	return "unknown"
}
