package providers

import (
	"fmt"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

func BuildSourceRuntime(cfg appconfig.Config, source appconfig.Source, tokenSource provider.TokenSource, options provider.SourceRuntimeOptions) (provider.SourceRuntime, error) {
	driver := normalizeDriver(source.Driver)
	source.Driver = driver

	sourceInfo, ok := LookupSourceInfo(driver)
	if !ok {
		return provider.SourceRuntime{}, fmt.Errorf("不支持的 source driver: %s", driver)
	}
	driverImpl, ok := LookupDriver(driver)
	if !ok {
		return provider.SourceRuntime{}, fmt.Errorf("不支持的 source driver: %s", driver)
	}
	if sourceInfo.RequiresCredential && tokenSource == nil {
		return provider.SourceRuntime{}, fmt.Errorf("source driver %s 需要 token source", driver)
	}
	if runtimeDriver, ok := driverImpl.(provider.SourceRuntimeDriver); ok {
		return runtimeDriver.BuildSourceRuntime(cfg, source, tokenSource, options)
	}
	sourceDriver, ok := driverImpl.(provider.SourceDriver)
	if !ok {
		return provider.SourceRuntime{}, fmt.Errorf("source driver %s 未提供 runtime 构建", driver)
	}
	clients, err := sourceDriver.BuildSource(cfg, source.Folder, tokenSource)
	if err != nil {
		return provider.SourceRuntime{}, err
	}
	return provider.SourceRuntime{Clients: clients}, nil
}

func BuildSourceClients(cfg appconfig.Config, driver, folder string, tokenSource provider.TokenSource) (provider.SourceClients, error) {
	runtime, err := BuildSourceRuntime(cfg, appconfig.Source{
		Driver: driver,
		Folder: folder,
		Mode:   "poll",
	}, tokenSource, provider.SourceRuntimeOptions{})
	if err != nil {
		return provider.SourceClients{}, err
	}
	if !hasSourceClients(runtime.Clients) {
		return provider.SourceClients{}, fmt.Errorf("source driver %s 未提供 provider clients", driver)
	}
	return runtime.Clients, nil
}

func BuildSinkClients(cfg appconfig.Config, driver, folder string, tokenSource provider.TokenSource) (provider.SinkClients, error) {
	driver = normalizeDriver(driver)
	sinkInfo, ok := LookupSinkInfo(driver)
	if !ok {
		return provider.SinkClients{}, fmt.Errorf("不支持的 sink driver: %s", driver)
	}
	driverImpl, ok := LookupDriver(driver)
	if !ok {
		return provider.SinkClients{}, fmt.Errorf("不支持的 sink driver: %s", driver)
	}
	sinkDriver, ok := driverImpl.(provider.SinkDriver)
	if !ok {
		return provider.SinkClients{}, fmt.Errorf("sink driver %s 未提供 provider clients", driver)
	}
	if sinkInfo.RequiresCredential && tokenSource == nil {
		return provider.SinkClients{}, fmt.Errorf("sink driver %s 需要 token source", driver)
	}
	return sinkDriver.BuildSink(cfg, folder, tokenSource)
}

func SessionAuthConfigForDrivers(cfg appconfig.Config, drivers ...string) appconfig.AuthConfig {
	authCfg := cfg.Auth

	needsGraph := false
	needsEWS := false
	needsIMAP := false
	for _, driver := range drivers {
		info, ok := LookupDriverInfo(driver)
		if !ok {
			continue
		}
		if info.Auth.Graph {
			needsGraph = true
		}
		if info.Auth.EWS {
			needsEWS = true
		}
		if info.Auth.IMAP {
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

func hasSourceClients(clients provider.SourceClients) bool {
	return clients.Reader != nil || clients.Deleter != nil
}
