package graph

import (
	"fmt"
	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
	"strings"

	imapprovider "mimecrypt/internal/providers/imap"
)

// Build 构造当前基于 Microsoft Graph 的 provider 实现。
func Build(cfg appconfig.Config) (provider.Clients, error) {
	authCfg := cfg.Auth
	if strings.ToLower(strings.TrimSpace(cfg.Mail.Pipeline.WriteBackProvider)) != "ews" {
		authCfg.EWSScopes = nil
	}

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

	var writer provider.Writer
	switch strings.ToLower(strings.TrimSpace(cfg.Mail.Pipeline.WriteBackProvider)) {
	case "", "graph":
		writer = sourceWriter
	case "ews":
		writer, err = newEWSWriter(cfg, session, nil)
	case "imap":
		writer, err = imapprovider.NewWriter(cfg, session)
	default:
		return provider.Clients{}, fmt.Errorf("不支持的回写后端: %s", cfg.Mail.Pipeline.WriteBackProvider)
	}
	if err != nil {
		return provider.Clients{}, err
	}

	return provider.Clients{
		Session: session,
		Reader:  client,
		Writer:  writer,
		Deleter: sourceWriter,
	}, nil
}
