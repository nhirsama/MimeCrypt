package appruntime

import (
	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/modules/download"
	"mimecrypt/internal/modules/health"
	"mimecrypt/internal/modules/list"
	"mimecrypt/internal/modules/login"
	"mimecrypt/internal/modules/logout"
	"mimecrypt/internal/modules/tokenstate"
	"mimecrypt/internal/providers"
)

func BuildLoginService(cfg appconfig.Config) (*login.Service, error) {
	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		return nil, err
	}
	sourceClients, err := providers.BuildSourceClientsWithSession(cfg, session)
	if err != nil {
		return nil, err
	}

	return &login.Service{
		Session:  sourceClients.Session,
		Mail:     sourceClients.Reader,
		StateDir: cfg.Auth.StateDir,
	}, nil
}

func BuildLogoutService(cfg appconfig.Config) (*logout.Service, error) {
	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		return nil, err
	}
	return &logout.Service{Session: session}, nil
}

func BuildDownloadService(cfg appconfig.Config) (*download.Service, error) {
	sourceClients, err := providers.BuildSourceClients(cfg)
	if err != nil {
		return nil, err
	}
	return &download.Service{Client: sourceClients.Reader}, nil
}

func BuildListService(cfg appconfig.Config) (*list.Service, error) {
	sourceClients, err := providers.BuildSourceClients(cfg)
	if err != nil {
		return nil, err
	}
	return &list.Service{Client: sourceClients.Reader}, nil
}

func BuildHealthService(cfg appconfig.Config) (*health.Service, error) {
	sourceClients, err := providers.BuildSourceClients(cfg)
	if err != nil {
		return nil, err
	}
	sinkClients, err := providers.BuildWriteBackClients(cfg)
	if err != nil {
		return nil, err
	}

	return &health.Service{
		StateDir:          cfg.Auth.StateDir,
		Folder:            cfg.Mail.Sync.Folder,
		Provider:          cfg.Provider,
		WriteBackProvider: cfg.Mail.Pipeline.WriteBackProvider,
		Session:           sourceClients.Session,
		Reader:            sourceClients.Reader,
		WriteBack:         sinkClients.Health,
	}, nil
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
