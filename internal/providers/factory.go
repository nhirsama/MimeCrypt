package providers

import (
	"fmt"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
)

func BuildSourceClients(cfg appconfig.Config, driver, folder string) (provider.SourceClients, error) {
	return BuildSourceClientsWithSession(cfg, driver, folder, nil)
}

func BuildSourceClientsWithSession(cfg appconfig.Config, driver, folder string, session provider.Session) (provider.SourceClients, error) {
	driver = normalizeDriver(driver)
	sourceSpec, ok := provider.LookupSourceSpec(driver)
	if !ok {
		return provider.SourceClients{}, fmt.Errorf("不支持的 source driver: %s", driver)
	}
	builder, ok := lookupDriverBuilder(driver)
	if !ok || builder.buildSource == nil {
		return provider.SourceClients{}, fmt.Errorf("source driver %s 未提供 provider clients", driver)
	}
	if session == nil && sourceSpec.RequiresCredential {
		var err error
		session, err = auth.NewSession(SessionAuthConfigForDrivers(cfg, driver), nil)
		if err != nil {
			return provider.SourceClients{}, err
		}
	}
	return builder.buildSource(cfg, folder, session)
}

func BuildSinkClients(cfg appconfig.Config, driver, folder string) (provider.SinkClients, error) {
	return BuildSinkClientsWithSession(cfg, driver, folder, nil)
}

func BuildSinkClientsWithSession(cfg appconfig.Config, driver, folder string, session provider.Session) (provider.SinkClients, error) {
	driver = normalizeDriver(driver)
	sinkSpec, ok := provider.LookupSinkSpec(driver)
	if !ok {
		return provider.SinkClients{}, fmt.Errorf("不支持的 sink driver: %s", driver)
	}
	builder, ok := lookupDriverBuilder(driver)
	if !ok || builder.buildSink == nil {
		return provider.SinkClients{}, fmt.Errorf("sink driver %s 未提供 provider clients", driver)
	}
	if session == nil && sinkSpec.RequiresCredential {
		var err error
		session, err = auth.NewSession(SessionAuthConfigForDrivers(cfg, driver), nil)
		if err != nil {
			return provider.SinkClients{}, err
		}
	}
	return builder.buildSink(cfg, folder, session)
}

func SessionAuthConfigForDrivers(cfg appconfig.Config, drivers ...string) appconfig.AuthConfig {
	authCfg := cfg.Auth

	needsGraph := false
	needsEWS := false
	needsIMAP := false
	for _, driver := range drivers {
		spec, ok := provider.LookupDriverSpec(driver)
		if !ok {
			continue
		}
		if spec.Auth.Graph {
			needsGraph = true
		}
		if spec.Auth.EWS {
			needsEWS = true
		}
		if spec.Auth.IMAP {
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
