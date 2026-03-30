package providers

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers/graph"
)

type CredentialSession interface {
	provider.TokenSource
	Login(context.Context, io.Writer) (provider.Token, error)
	Logout() error
	LoadCachedToken() (provider.Token, error)
	StoreToken(provider.Token) error
}

type CredentialRemoteRevoker interface {
	Revoke(context.Context, io.Writer) error
}

type LoginRuntime struct {
	Config        appconfig.Config
	Session       CredentialSession
	IdentityProbe func(context.Context) (provider.User, error)
	Drivers       []string
	ConfigName    string
}

type driverLoginConfig struct {
	Name               string
	ApplyConfig        func(appconfig.Config, []string) appconfig.Config
	BuildSession       func(appconfig.Config) (CredentialSession, error)
	BuildIdentityProbe func(appconfig.Config, []string, provider.TokenSource) (func(context.Context) (provider.User, error), error)
}

type driverRevokeConfig struct {
	Name               string
	ApplyConfig        func(appconfig.Config, []string) appconfig.Config
	BuildRemoteRevoker func(appconfig.Config, []string, provider.TokenSource) (CredentialRemoteRevoker, error)
}

var microsoftDriverLoginConfig = &driverLoginConfig{
	Name: "microsoft-oauth",
	ApplyConfig: func(cfg appconfig.Config, drivers []string) appconfig.Config {
		cfg.Auth = applyMicrosoftDriverAuthConfig(cfg.Auth, drivers)
		return cfg
	},
	BuildSession: func(cfg appconfig.Config) (CredentialSession, error) {
		return auth.NewSession(cfg.Auth, nil)
	},
	BuildIdentityProbe: buildMicrosoftLoginIdentityProbe,
}

var microsoftDriverRevokeConfig = &driverRevokeConfig{
	Name: "microsoft-oauth",
	ApplyConfig: func(cfg appconfig.Config, drivers []string) appconfig.Config {
		cfg.Auth = applyMicrosoftDriverAuthConfig(cfg.Auth, drivers)
		return cfg
	},
	BuildRemoteRevoker: func(cfg appconfig.Config, _ []string, tokenSource provider.TokenSource) (CredentialRemoteRevoker, error) {
		return graph.NewIdentityRevoker(cfg, tokenSource, nil)
	},
}

func BuildLoginRuntime(cfg appconfig.Config, drivers ...string) (LoginRuntime, error) {
	resolvedDrivers := effectiveCredentialDrivers(cfg, drivers...)
	loginConfig, resolvedDrivers, err := resolveDriverLoginConfig(resolvedDrivers)
	if err != nil {
		return LoginRuntime{}, err
	}
	if loginConfig == nil {
		return LoginRuntime{}, fmt.Errorf("未找到可用的 login 驱动配置")
	}

	effectiveCfg := cfg
	if loginConfig.ApplyConfig != nil {
		effectiveCfg = loginConfig.ApplyConfig(cfg, resolvedDrivers)
	}

	session, err := loginConfig.BuildSession(effectiveCfg)
	if err != nil {
		return LoginRuntime{}, err
	}

	var identityProbe func(context.Context) (provider.User, error)
	if loginConfig.BuildIdentityProbe != nil {
		identityProbe, err = loginConfig.BuildIdentityProbe(effectiveCfg, resolvedDrivers, session)
		if err != nil {
			return LoginRuntime{}, err
		}
	}

	return LoginRuntime{
		Config:        effectiveCfg,
		Session:       session,
		IdentityProbe: identityProbe,
		Drivers:       resolvedDrivers,
		ConfigName:    loginConfig.Name,
	}, nil
}

func BuildRemoteRevoker(cfg appconfig.Config, tokenSource provider.TokenSource, drivers ...string) (CredentialRemoteRevoker, appconfig.Config, error) {
	resolvedDrivers := effectiveCredentialDrivers(cfg, drivers...)
	revokeConfig, resolvedDrivers, err := resolveDriverRevokeConfig(resolvedDrivers)
	if err != nil {
		return nil, appconfig.Config{}, err
	}
	if revokeConfig == nil {
		return nil, cfg, fmt.Errorf("未找到可用的 revoke 驱动配置")
	}

	effectiveCfg := cfg
	if revokeConfig.ApplyConfig != nil {
		effectiveCfg = revokeConfig.ApplyConfig(cfg, resolvedDrivers)
	}

	revoker, err := revokeConfig.BuildRemoteRevoker(effectiveCfg, resolvedDrivers, tokenSource)
	if err != nil {
		return nil, appconfig.Config{}, err
	}
	return revoker, effectiveCfg, nil
}

func buildMicrosoftLoginIdentityProbe(cfg appconfig.Config, _ []string, tokenSource provider.TokenSource) (func(context.Context) (provider.User, error), error) {
	switch {
	case len(cfg.Auth.GraphScopes) > 0:
		clients, err := graph.BuildSourceClientsWithTokenSource(cfg, "", tokenSource)
		if err != nil {
			return nil, err
		}
		return clients.Reader.Me, nil
	case strings.TrimSpace(cfg.Mail.Client.IMAPUsername) != "":
		user := provider.User{
			Mail:              cfg.Mail.Client.IMAPUsername,
			UserPrincipalName: cfg.Mail.Client.IMAPUsername,
			DisplayName:       cfg.Mail.Client.IMAPUsername,
		}
		return func(context.Context) (provider.User, error) {
			return user, nil
		}, nil
	default:
		return nil, nil
	}
}

func applyMicrosoftDriverAuthConfig(authCfg appconfig.AuthConfig, drivers []string) appconfig.AuthConfig {
	needsGraph := false
	needsEWS := false
	needsIMAP := false
	for _, driver := range uniqueNormalizedDrivers(drivers) {
		switch driver {
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

func effectiveCredentialDrivers(cfg appconfig.Config, drivers ...string) []string {
	normalized := uniqueNormalizedDrivers(drivers)
	if len(normalized) > 0 {
		return normalized
	}

	inferred := make([]string, 0, 3)
	if len(cfg.Auth.GraphScopes) > 0 {
		inferred = append(inferred, "graph")
	}
	if len(cfg.Auth.EWSScopes) > 0 {
		inferred = append(inferred, "ews")
	}
	if len(cfg.Auth.IMAPScopes) > 0 {
		inferred = append(inferred, "imap")
	}
	return uniqueNormalizedDrivers(inferred)
}

func uniqueNormalizedDrivers(drivers []string) []string {
	seen := make(map[string]struct{}, len(drivers))
	normalized := make([]string, 0, len(drivers))
	for _, driver := range drivers {
		driver = normalizeDriver(driver)
		if driver == "" {
			continue
		}
		if _, ok := seen[driver]; ok {
			continue
		}
		seen[driver] = struct{}{}
		normalized = append(normalized, driver)
	}
	sort.Strings(normalized)
	return normalized
}

func resolveDriverLoginConfig(drivers []string) (*driverLoginConfig, []string, error) {
	var selected *driverLoginConfig
	for _, driver := range uniqueNormalizedDrivers(drivers) {
		registration, ok := lookupDriverRegistration(driver)
		if !ok {
			return nil, nil, fmt.Errorf("不支持的 driver: %s", driver)
		}
		if registration.LoginConfig == nil {
			continue
		}
		if selected == nil {
			selected = registration.LoginConfig
			continue
		}
		if selected.Name != registration.LoginConfig.Name {
			return nil, nil, fmt.Errorf("driver %s 与 %s 需要不同的 login 配置，当前 credential 无法共享", drivers[0], driver)
		}
	}
	return selected, uniqueNormalizedDrivers(drivers), nil
}

func resolveDriverRevokeConfig(drivers []string) (*driverRevokeConfig, []string, error) {
	var selected *driverRevokeConfig
	for _, driver := range uniqueNormalizedDrivers(drivers) {
		registration, ok := lookupDriverRegistration(driver)
		if !ok {
			return nil, nil, fmt.Errorf("不支持的 driver: %s", driver)
		}
		if registration.RevokeConfig == nil {
			continue
		}
		if selected == nil {
			selected = registration.RevokeConfig
			continue
		}
		if selected.Name != registration.RevokeConfig.Name {
			return nil, nil, fmt.Errorf("driver %s 与 %s 需要不同的 revoke 配置，当前 credential 无法共享", drivers[0], driver)
		}
	}
	return selected, uniqueNormalizedDrivers(drivers), nil
}
