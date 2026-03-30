package providers

import (
	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers/graph"
	"mimecrypt/internal/providers/imap"
)

type sourceClientsFactory func(cfg appconfig.Config, folder string, session provider.Session) (provider.SourceClients, error)
type sinkClientsFactory func(cfg appconfig.Config, folder string, session provider.Session) (provider.SinkClients, error)

type driverBuilder struct {
	buildSource sourceClientsFactory
	buildSink   sinkClientsFactory
}

var driverBuilders = map[string]driverBuilder{
	"ews": {
		buildSink: func(cfg appconfig.Config, _ string, session provider.Session) (provider.SinkClients, error) {
			return graph.NewEWSWriterClients(cfg, session)
		},
	},
	"graph": {
		buildSource: func(cfg appconfig.Config, _ string, session provider.Session) (provider.SourceClients, error) {
			return graph.BuildSourceClientsWithSession(cfg, session)
		},
		buildSink: func(cfg appconfig.Config, _ string, session provider.Session) (provider.SinkClients, error) {
			return graph.NewWriterClients(cfg, session)
		},
	},
	"imap": {
		buildSource: imap.BuildSourceClientsWithSession,
		buildSink:   imap.NewWriterClients,
	},
}

func lookupDriverBuilder(driver string) (driverBuilder, bool) {
	builder, ok := driverBuilders[normalizeDriver(driver)]
	return builder, ok
}
