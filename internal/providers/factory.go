package providers

import (
	"fmt"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

func BuildSourceClients(cfg appconfig.Config, driver, folder string, tokenSource provider.TokenSource) (provider.SourceClients, error) {
	driver = normalizeDriver(driver)
	sourceSpec, ok := LookupSourceSpec(driver)
	if !ok {
		return provider.SourceClients{}, fmt.Errorf("不支持的 source driver: %s", driver)
	}
	registration, ok := lookupDriverRegistration(driver)
	if !ok || registration.BuildSource == nil {
		return provider.SourceClients{}, fmt.Errorf("source driver %s 未提供 provider clients", driver)
	}
	if sourceSpec.RequiresCredential && tokenSource == nil {
		return provider.SourceClients{}, fmt.Errorf("source driver %s 需要 token source", driver)
	}
	return registration.BuildSource(cfg, folder, tokenSource)
}

func BuildSinkClients(cfg appconfig.Config, driver, folder string, tokenSource provider.TokenSource) (provider.SinkClients, error) {
	driver = normalizeDriver(driver)
	sinkSpec, ok := LookupSinkSpec(driver)
	if !ok {
		return provider.SinkClients{}, fmt.Errorf("不支持的 sink driver: %s", driver)
	}
	registration, ok := lookupDriverRegistration(driver)
	if !ok || registration.BuildSink == nil {
		return provider.SinkClients{}, fmt.Errorf("sink driver %s 未提供 provider clients", driver)
	}
	if sinkSpec.RequiresCredential && tokenSource == nil {
		return provider.SinkClients{}, fmt.Errorf("sink driver %s 需要 token source", driver)
	}
	return registration.BuildSink(cfg, folder, tokenSource)
}

func SessionAuthConfigForDrivers(cfg appconfig.Config, drivers ...string) appconfig.AuthConfig {
	authCfg := cfg.Auth

	needsGraph := false
	needsEWS := false
	needsIMAP := false
	for _, driver := range drivers {
		spec, ok := LookupDriverSpec(driver)
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
