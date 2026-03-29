package graph

import (
	"context"
	"fmt"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
)

func BuildSourceClients(cfg appconfig.Config) (provider.SourceClients, error) {
	authCfg := cfg.Auth
	authCfg.EWSScopes = nil
	authCfg.IMAPScopes = nil

	session, err := auth.NewSession(authCfg, nil)
	if err != nil {
		return provider.SourceClients{}, err
	}
	return BuildSourceClientsWithSession(cfg, session)
}

func BuildSourceClientsWithSession(cfg appconfig.Config, session provider.Session) (provider.SourceClients, error) {
	if session == nil {
		return provider.SourceClients{}, fmt.Errorf("session 不能为空")
	}

	tokenSource := graphTokenSource{session: session, scopes: append([]string(nil), cfg.Auth.GraphScopes...)}
	client, err := newReader(cfg.Mail.Client, tokenSource, nil)
	if err != nil {
		return provider.SourceClients{}, err
	}
	sourceWriter, err := newWriter(cfg.Mail.Client, tokenSource, nil)
	if err != nil {
		return provider.SourceClients{}, err
	}

	return provider.SourceClients{
		Session: session,
		Reader:  client,
		Deleter: sourceWriter,
	}, nil
}

func NewWriterClients(cfg appconfig.Config, session provider.Session) (provider.SinkClients, error) {
	if session == nil {
		return provider.SinkClients{}, fmt.Errorf("session 不能为空")
	}
	tokenSource := graphTokenSource{session: session, scopes: append([]string(nil), cfg.Auth.GraphScopes...)}
	reader, err := newReader(cfg.Mail.Client, tokenSource, nil)
	if err != nil {
		return provider.SinkClients{}, err
	}
	writer, err := newWriter(cfg.Mail.Client, tokenSource, nil)
	if err != nil {
		return provider.SinkClients{}, err
	}
	return provider.SinkClients{
		Session: session,
		Reader:  reader,
		Writer:  writer,
		Health:  writer,
	}, nil
}

func NewEWSWriterClients(cfg appconfig.Config, session provider.Session) (provider.SinkClients, error) {
	if session == nil {
		return provider.SinkClients{}, fmt.Errorf("session 不能为空")
	}
	reader, err := newReader(cfg.Mail.Client, session, nil)
	if err != nil {
		return provider.SinkClients{}, err
	}
	writer, err := newEWSWriter(cfg, session, nil)
	if err != nil {
		return provider.SinkClients{}, err
	}
	return provider.SinkClients{
		Session: session,
		Reader:  reader,
		Writer:  writer,
		Health:  writer,
	}, nil
}

type graphTokenSource struct {
	session provider.Session
	scopes  []string
}

func (s graphTokenSource) AccessToken(ctx context.Context) (string, error) {
	if len(s.scopes) > 0 {
		return s.session.AccessTokenForScopes(ctx, s.scopes)
	}
	return s.session.AccessToken(ctx)
}
