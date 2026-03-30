package appruntime

import (
	"context"
	"fmt"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/modules/login"
	"mimecrypt/internal/modules/revoke"
	"mimecrypt/internal/modules/tokenstate"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers"
	"mimecrypt/internal/providers/graph"
)

func BuildLoginService(plan CredentialPlan) (*login.Service, error) {
	cfg := effectiveLoginConfig(plan)

	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		return nil, err
	}

	service := &login.Service{
		Session:  session,
		StateDir: cfg.Auth.StateDir,
	}

	identityProbe, err := buildLoginIdentityProbe(cfg, session)
	if err != nil {
		return nil, err
	}
	service.IdentityProbe = identityProbe
	return service, nil
}

func effectiveLoginConfig(plan CredentialPlan) appconfig.Config {
	cfg := plan.Config
	cfg.Auth = loginAuthConfig(plan)
	return cfg
}

func loginAuthConfig(plan CredentialPlan) appconfig.AuthConfig {
	if len(plan.AuthDrivers) == 0 {
		return plan.Config.Auth
	}
	return providers.SessionAuthConfigForDrivers(plan.Config, plan.AuthDrivers...)
}

func buildLoginIdentityProbe(cfg appconfig.Config, tokenSource provider.TokenSource) (func(context.Context) (provider.User, error), error) {
	switch {
	case len(cfg.Auth.GraphScopes) > 0:
		clients, err := providers.BuildSourceClients(cfg, "graph", "", tokenSource)
		if err != nil {
			return nil, err
		}
		return clients.Reader.Me, nil
	case cfg.Mail.Client.IMAPUsername != "":
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

func BuildRevokeService(plan CredentialPlan, force bool) (*revoke.Service, error) {
	cfg := plan.Config

	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		return nil, err
	}

	kind, kindSpec, err := effectiveRevokeCredentialKind(plan)
	if err != nil {
		return nil, err
	}

	service := &revoke.Service{
		Session: session,
		ClearLocal: func() error {
			return appconfig.ClearLocalConfig(cfg.Auth.StateDir)
		},
		Force:         force,
		RequireRemote: kindSpec.RequiresRemoteRevoke,
	}
	if !kindSpec.RequiresRemoteRevoke {
		return service, nil
	}

	remoteRevoker, err := buildRemoteRevoker(cfg, kind, session)
	if err != nil {
		if !force {
			return nil, fmt.Errorf("初始化远端吊销器失败: %w", err)
		}
		service.RemotePrepareErr = fmt.Errorf("初始化远端吊销器失败: %w", err)
		return service, nil
	}
	service.RemoteRevoker = remoteRevoker
	return service, nil
}

func effectiveRevokeCredentialKind(plan CredentialPlan) (string, appconfig.CredentialKindSpec, error) {
	kind := strings.TrimSpace(plan.Credential.Kind)
	if kind == "" {
		kind = appconfig.CredentialKindOAuth
	}
	spec, ok := appconfig.LookupCredentialKindSpec(kind)
	if !ok {
		return "", appconfig.CredentialKindSpec{}, fmt.Errorf("credential kind 不支持: %s", kind)
	}
	return appconfig.NormalizeCredentialKind(kind), spec, nil
}

func buildRemoteRevoker(cfg appconfig.Config, credentialKind string, tokenSource provider.TokenSource) (revoke.RemoteRevoker, error) {
	kind := appconfig.NormalizeCredentialKind(credentialKind)
	spec, ok := appconfig.LookupCredentialKindSpec(kind)
	if !ok {
		return nil, fmt.Errorf("credential kind 不支持: %s", credentialKind)
	}
	if !spec.RequiresRemoteRevoke {
		return nil, nil
	}
	switch kind {
	case appconfig.CredentialKindOAuth:
		return graph.NewIdentityRevoker(cfg, tokenSource, nil)
	default:
		return nil, fmt.Errorf("credential kind %s 未提供远端吊销实现", kind)
	}
}

func BuildTokenStateService(cfg appconfig.Config) (*tokenstate.Service, error) {
	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		return nil, err
	}

	return &tokenstate.Service{
		Session:        session,
		StateDir:       cfg.Auth.StateDir,
		TokenStore:     cfg.Auth.TokenStoreMode(),
		KeyringService: cfg.Auth.KeyringServiceName(),
	}, nil
}
