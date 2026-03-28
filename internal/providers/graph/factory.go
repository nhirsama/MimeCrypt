package graph

import (
	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
)

// Build 构造当前基于 Microsoft Graph 的 provider 实现。
func Build(cfg appconfig.Config) (provider.Clients, error) {
	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		return provider.Clients{}, err
	}

	client, err := newReader(cfg.Mail.Client, session, nil)
	if err != nil {
		return provider.Clients{}, err
	}

	writer, err := newWriter(cfg.Mail.Client, session, nil)
	if err != nil {
		return provider.Clients{}, err
	}

	return provider.Clients{
		Session: session,
		Reader:  client,
		Writer:  writer,
	}, nil
}
