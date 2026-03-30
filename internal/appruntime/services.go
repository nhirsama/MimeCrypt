package appruntime

import (
	"context"
	"fmt"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/modules/login"
	"mimecrypt/internal/modules/revoke"
	"mimecrypt/internal/modules/tokenstate"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers"
	"mimecrypt/internal/providers/graph"
)

func BuildLoginService(cfg appconfig.Config) (*login.Service, error) {
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

func buildLoginIdentityProbe(cfg appconfig.Config, session provider.Session) (func(context.Context) (provider.User, error), error) {
	switch {
	case len(cfg.Auth.GraphScopes) > 0:
		clients, err := providers.BuildSourceClientsWithSession(cfg, "graph", "", session)
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

func BuildRevokeService(cfg appconfig.Config, force bool) (*revoke.Service, error) {
	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		return nil, err
	}

	service := &revoke.Service{
		Session: session,
		ClearLocal: func() error {
			return appconfig.ClearLocalConfig(cfg.Auth.StateDir)
		},
		Force: force,
	}

	remoteRevoker, err := graph.NewIdentityRevoker(cfg, session, nil)
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
