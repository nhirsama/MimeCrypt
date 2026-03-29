package graph

import (
	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
)

// Build 构造当前基于 Microsoft Graph 的 provider 实现。
func Build(cfg appconfig.Config) (provider.Clients, error) {
	authCfg := cfg.Auth
	authCfg.EWSScopes = nil
	authCfg.IMAPScopes = nil

	session, err := auth.NewSession(authCfg, nil)
	if err != nil {
		return provider.Clients{}, err
	}

	client, err := newReader(cfg.Mail.Client, session, nil)
	if err != nil {
		return provider.Clients{}, err
	}
	sourceWriter, err := newWriter(cfg.Mail.Client, session, nil)
	if err != nil {
		return provider.Clients{}, err
	}

	return provider.Clients{
		Session: session,
		Reader:  client,
		Writer:  sourceWriter,
		Deleter: sourceWriter,
	}, nil
}

func NewWriter(cfg appconfig.Config, tokenSource accessTokenSource) (provider.Writer, error) {
	return newWriter(cfg.Mail.Client, tokenSource, nil)
}

func NewEWSWriter(cfg appconfig.Config, tokenSource scopedAccessTokenSource) (provider.Writer, error) {
	return newEWSWriter(cfg, tokenSource, nil)
}
