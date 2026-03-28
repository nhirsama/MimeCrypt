package graph

import (
	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
)

// Build 构造当前基于 Microsoft Graph 的 provider 实现。
func Build(cfg appconfig.Config) (provider.Session, provider.Reader, provider.Writer, error) {
	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	client, err := newReader(cfg.Mail, session, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	writer, err := newWriter(cfg.Mail, session, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	return session, client, writer, nil
}
