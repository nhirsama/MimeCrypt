package graph

import (
	"context"
	"fmt"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

func BuildSourceClientsWithTokenSource(cfg appconfig.Config, _ string, tokenSource provider.TokenSource) (provider.SourceClients, error) {
	if tokenSource == nil {
		return provider.SourceClients{}, fmt.Errorf("token source 不能为空")
	}

	scopedSource := graphTokenSource{tokenSource: tokenSource, scopes: append([]string(nil), cfg.Auth.GraphScopes...)}
	client, err := newReader(cfg.Mail.Client, scopedSource, nil)
	if err != nil {
		return provider.SourceClients{}, err
	}
	sourceWriter, err := newWriter(cfg.Mail.Client, scopedSource, nil)
	if err != nil {
		return provider.SourceClients{}, err
	}

	return provider.SourceClients{
		Reader:  client,
		Deleter: sourceWriter,
	}, nil
}

func NewWriterClients(cfg appconfig.Config, _ string, tokenSource provider.TokenSource) (provider.SinkClients, error) {
	if tokenSource == nil {
		return provider.SinkClients{}, fmt.Errorf("token source 不能为空")
	}
	scopedSource := graphTokenSource{tokenSource: tokenSource, scopes: append([]string(nil), cfg.Auth.GraphScopes...)}
	reader, err := newReader(cfg.Mail.Client, scopedSource, nil)
	if err != nil {
		return provider.SinkClients{}, err
	}
	writer, err := newWriter(cfg.Mail.Client, scopedSource, nil)
	if err != nil {
		return provider.SinkClients{}, err
	}
	return provider.SinkClients{
		Reader:     reader,
		Writer:     writer,
		Reconciler: writer,
		Health:     writer,
	}, nil
}

func NewEWSWriterClients(cfg appconfig.Config, _ string, tokenSource provider.TokenSource) (provider.SinkClients, error) {
	if tokenSource == nil {
		return provider.SinkClients{}, fmt.Errorf("token source 不能为空")
	}
	reader, err := newReader(cfg.Mail.Client, tokenSource, nil)
	if err != nil {
		return provider.SinkClients{}, err
	}
	writer, err := newEWSWriter(cfg, tokenSource, nil)
	if err != nil {
		return provider.SinkClients{}, err
	}
	return provider.SinkClients{
		Reader:     reader,
		Writer:     writer,
		Reconciler: writer,
		Health:     writer,
	}, nil
}

type graphTokenSource struct {
	tokenSource provider.TokenSource
	scopes      []string
}

func (s graphTokenSource) AccessToken(ctx context.Context) (string, error) {
	if len(s.scopes) > 0 {
		return s.tokenSource.AccessTokenForScopes(ctx, s.scopes)
	}
	return s.tokenSource.AccessToken(ctx)
}
