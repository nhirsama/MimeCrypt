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

func BuildSourceClients(cfg appconfig.Config, driver, folder string) (provider.SourceClients, error) {
	return BuildSourceClientsWithSession(cfg, driver, folder, nil)
}

func BuildSourceClientsWithSession(cfg appconfig.Config, driver, folder string, session provider.Session) (provider.SourceClients, error) {
	driver = normalizeDriver(driver)
	if session == nil {
		var err error
		session, err = auth.NewSession(SessionAuthConfigForDrivers(cfg, driver), nil)
		if err != nil {
			return provider.SourceClients{}, err
		}
	}

	switch driver {
	case "graph":
		return graph.BuildSourceClientsWithSession(cfg, session)
	case "imap":
		return imap.BuildSourceClientsWithSession(cfg, folder, session)
	default:
		return provider.SourceClients{}, fmt.Errorf("不支持的 source driver: %s", driver)
	}
}

func BuildSinkClients(cfg appconfig.Config, driver, folder string) (provider.SinkClients, error) {
	return BuildSinkClientsWithSession(cfg, driver, folder, nil)
}

func BuildSinkClientsWithSession(cfg appconfig.Config, driver, folder string, session provider.Session) (provider.SinkClients, error) {
	driver = normalizeDriver(driver)
	if session == nil {
		var err error
		session, err = auth.NewSession(SessionAuthConfigForDrivers(cfg, driver), nil)
		if err != nil {
			return provider.SinkClients{}, err
		}
	}

	switch driver {
	case "graph":
		return graph.NewWriterClients(cfg, session)
	case "ews":
		return graph.NewEWSWriterClients(cfg, session)
	case "imap":
		return imap.NewWriterClients(cfg, folder, session)
	default:
		return provider.SinkClients{}, fmt.Errorf("不支持的 sink driver: %s", driver)
	}
}

func SessionAuthConfigForDrivers(cfg appconfig.Config, drivers ...string) appconfig.AuthConfig {
	authCfg := cfg.Auth

	needsGraph := false
	needsEWS := false
	needsIMAP := false
	for _, driver := range drivers {
		switch normalizeDriver(driver) {
		case "graph":
			needsGraph = true
		case "ews":
			needsGraph = true
			needsEWS = true
		case "imap":
			needsIMAP = true
		}
	}

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

func normalizeDriver(driver string) string {
	return strings.ToLower(strings.TrimSpace(driver))
}
