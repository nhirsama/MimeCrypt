package graph

import (
	"fmt"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
)

// Build 构造当前基于 Microsoft Graph 的 provider 实现。
func Build(cfg appconfig.Config) (provider.Clients, error) {
	source, err := BuildSourceClients(cfg)
	if err != nil {
		return provider.Clients{}, err
	}
	sink, err := NewWriterClients(cfg, source.Session)
	if err != nil {
		return provider.Clients{}, err
	}
	return provider.Clients{
		Session:    source.Session,
		Reader:     source.Reader,
		Writer:     sink.Writer,
		Deleter:    source.Deleter,
		Reconciler: sink.Reconciler,
		Health:     sink.Health,
	}, nil
}

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

	client, err := newReader(cfg.Mail.Client, session, nil)
	if err != nil {
		return provider.SourceClients{}, err
	}
	sourceWriter, err := newWriter(cfg.Mail.Client, session, nil)
	if err != nil {
		return provider.SourceClients{}, err
	}

	return provider.SourceClients{
		Session: session,
		Reader:  client,
		Deleter: sourceWriter,
	}, nil
}

func BuildWithSession(cfg appconfig.Config, session provider.Session) (provider.Clients, error) {
	source, err := BuildSourceClientsWithSession(cfg, session)
	if err != nil {
		return provider.Clients{}, err
	}
	sink, err := NewWriterClients(cfg, source.Session)
	if err != nil {
		return provider.Clients{}, err
	}
	return provider.Clients{
		Session:    source.Session,
		Reader:     source.Reader,
		Writer:     sink.Writer,
		Deleter:    source.Deleter,
		Reconciler: sink.Reconciler,
		Health:     sink.Health,
	}, nil
}

func NewWriterClients(cfg appconfig.Config, session provider.Session) (provider.SinkClients, error) {
	if session == nil {
		return provider.SinkClients{}, fmt.Errorf("session 不能为空")
	}
	reader, err := newReader(cfg.Mail.Client, session, nil)
	if err != nil {
		return provider.SinkClients{}, err
	}
	writer, err := newWriter(cfg.Mail.Client, session, nil)
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

func NewEWSWriterClients(cfg appconfig.Config, session provider.ScopedSession) (provider.SinkClients, error) {
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

func NewWriter(cfg appconfig.Config, tokenSource accessTokenSource) (provider.Writer, error) {
	writer, err := newWriter(cfg.Mail.Client, tokenSource, nil)
	if err != nil {
		return nil, err
	}
	return writer, nil
}

func NewEWSWriter(cfg appconfig.Config, tokenSource provider.ScopedSession) (provider.Writer, error) {
	writer, err := newEWSWriter(cfg, tokenSource, nil)
	if err != nil {
		return nil, err
	}
	return writer, nil
}
