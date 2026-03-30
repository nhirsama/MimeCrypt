package providers

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/mailflow/adapters"
	"mimecrypt/internal/modules/audit"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers/graph"
	"mimecrypt/internal/providers/imap"
	webhookdevice "mimecrypt/internal/providers/webhook"
)

type PushIngress interface {
	Run(ctx context.Context) error
}

type sourceClientsFactory func(cfg appconfig.Config, folder string, tokenSource provider.TokenSource) (provider.SourceClients, error)
type sinkClientsFactory func(cfg appconfig.Config, folder string, tokenSource provider.TokenSource) (provider.SinkClients, error)
type pushIngressFactory func(cfg appconfig.Config, route appconfig.Route, source appconfig.Source, spool *adapters.PushSpool) (PushIngress, error)
type localConsumerFactory func(sink appconfig.Sink, cfg appconfig.Config, auditor auditRecorder) (mailflow.Consumer, error)

type auditRecorder interface {
	Record(event audit.Event) error
}

type driverRegistration struct {
	Spec             provider.DriverSpec
	LoginConfig      *driverLoginConfig
	RevokeConfig     *driverRevokeConfig
	BuildSource      sourceClientsFactory
	BuildSink        sinkClientsFactory
	BuildPushIngress pushIngressFactory
	BuildLocalSink   localConsumerFactory
	ConfigureSource  func(appconfig.Source, io.Reader, io.Writer) (appconfig.Source, error)
	DescribeSource   func(appconfig.Source) []string
	ValidateSource   func(source appconfig.Source) error
	ValidateSink     func(sink appconfig.Sink) error
}

var driverRegistrations = map[string]driverRegistration{
	"backup": {
		Spec: provider.DriverSpec{
			Name: "backup",
			Sink: &provider.SinkSpec{
				RequiresOutputDir: true,
				LocalConsumer:     true,
				LocalConsumerKind: provider.LocalConsumerBackup,
			},
		},
		BuildLocalSink: func(sink appconfig.Sink, _ appconfig.Config, auditor auditRecorder) (mailflow.Consumer, error) {
			return &adapters.BackupConsumer{
				OutputDir: sink.OutputDir,
				Auditor:   auditor,
			}, nil
		},
	},
	"discard": {
		Spec: provider.DriverSpec{
			Name: "discard",
			Sink: &provider.SinkSpec{
				LocalConsumer:     true,
				LocalConsumerKind: provider.LocalConsumerDiscard,
			},
		},
		BuildLocalSink: func(appconfig.Sink, appconfig.Config, auditRecorder) (mailflow.Consumer, error) {
			return &adapters.DiscardConsumer{}, nil
		},
	},
	"ews": {
		Spec: provider.DriverSpec{
			Name: "ews",
			Auth: provider.AuthRequirement{
				Graph: true,
				EWS:   true,
			},
			Sink: &provider.SinkSpec{
				RequiresCredential: true,
				SupportsVerify:     true,
				SupportsReconcile:  true,
				SupportsHealth:     true,
			},
		},
		LoginConfig:  microsoftDriverLoginConfig,
		RevokeConfig: microsoftDriverRevokeConfig,
		BuildSink:    graph.NewEWSWriterClients,
	},
	"file": {
		Spec: provider.DriverSpec{
			Name: "file",
			Sink: &provider.SinkSpec{
				RequiresOutputDir: true,
				LocalConsumer:     true,
				LocalConsumerKind: provider.LocalConsumerFile,
			},
		},
		BuildLocalSink: func(sink appconfig.Sink, _ appconfig.Config, _ auditRecorder) (mailflow.Consumer, error) {
			return &adapters.FileConsumer{OutputDir: sink.OutputDir}, nil
		},
	},
	"graph": {
		Spec: provider.DriverSpec{
			Name: "graph",
			Auth: provider.AuthRequirement{
				Graph: true,
			},
			Source: &provider.SourceSpec{
				RequiresCredential: true,
				SupportsDelete:     true,
				DeleteSemantics:    provider.DeleteSemanticsSoft,
				ProbeKind:          provider.ProviderProbeIdentity,
				Modes: map[string]provider.SourceModeSpec{
					"poll": {
						RequiresStatePath:    true,
						RequiresPollInterval: true,
						RequiresCycleTimeout: true,
					},
				},
			},
			Sink: &provider.SinkSpec{
				RequiresCredential: true,
				SupportsVerify:     true,
				SupportsReconcile:  true,
				SupportsHealth:     true,
			},
		},
		LoginConfig:  microsoftDriverLoginConfig,
		RevokeConfig: microsoftDriverRevokeConfig,
		BuildSource:  graph.BuildSourceClientsWithTokenSource,
		BuildSink:    graph.NewWriterClients,
	},
	"imap": {
		Spec: provider.DriverSpec{
			Name: "imap",
			Auth: provider.AuthRequirement{
				IMAP: true,
			},
			Source: &provider.SourceSpec{
				RequiresCredential: true,
				SupportsDelete:     true,
				DeleteSemantics:    provider.DeleteSemanticsHard,
				ProbeKind:          provider.ProviderProbeFolderList,
				Modes: map[string]provider.SourceModeSpec{
					"poll": {
						RequiresStatePath:    true,
						RequiresPollInterval: true,
						RequiresCycleTimeout: true,
					},
				},
			},
			Sink: &provider.SinkSpec{
				RequiresCredential: true,
				SupportsVerify:     true,
				SupportsReconcile:  true,
				SupportsHealth:     true,
			},
		},
		LoginConfig:  microsoftDriverLoginConfig,
		RevokeConfig: microsoftDriverRevokeConfig,
		BuildSource:  imap.BuildSourceClientsWithTokenSource,
		BuildSink:    imap.NewWriterClients,
	},
	"webhook": {
		Spec: provider.DriverSpec{
			Name: "webhook",
			Source: &provider.SourceSpec{
				SupportsDelete:  false,
				DeleteSemantics: provider.DeleteSemanticsUnknown,
				Modes: map[string]provider.SourceModeSpec{
					"push": {},
				},
			},
		},
		BuildPushIngress: func(cfg appconfig.Config, route appconfig.Route, source appconfig.Source, spool *adapters.PushSpool) (PushIngress, error) {
			return webhookdevice.BuildIngress(cfg, route, source, spool)
		},
		ConfigureSource: webhookdevice.ConfigureSource,
		DescribeSource:  webhookdevice.DescribeSource,
		ValidateSource:  webhookdevice.ValidateSourceConfig,
	},
}

func LookupDriverSpec(driver string) (provider.DriverSpec, bool) {
	registration, ok := lookupDriverRegistration(driver)
	if !ok {
		return provider.DriverSpec{}, false
	}
	return registration.Spec, true
}

func LookupSourceSpec(driver string) (*provider.SourceSpec, bool) {
	spec, ok := LookupDriverSpec(driver)
	if !ok || spec.Source == nil {
		return nil, false
	}
	return spec.Source, true
}

func LookupSinkSpec(driver string) (*provider.SinkSpec, bool) {
	spec, ok := LookupDriverSpec(driver)
	if !ok || spec.Sink == nil {
		return nil, false
	}
	return spec.Sink, true
}

func AllDriverSpecs() []provider.DriverSpec {
	names := make([]string, 0, len(driverRegistrations))
	for name := range driverRegistrations {
		names = append(names, name)
	}
	sort.Strings(names)

	specs := make([]provider.DriverSpec, 0, len(names))
	for _, name := range names {
		specs = append(specs, driverRegistrations[name].Spec)
	}
	return specs
}

func BuildPushIngress(cfg appconfig.Config, route appconfig.Route, source appconfig.Source, spool *adapters.PushSpool) (PushIngress, error) {
	registration, ok := lookupDriverRegistration(source.Driver)
	if !ok || registration.BuildPushIngress == nil {
		return nil, fmt.Errorf("push source driver 未提供 ingress 实现: %s", source.Driver)
	}
	return registration.BuildPushIngress(cfg, route, source, spool)
}

func BuildLocalConsumer(cfg appconfig.Config, sink appconfig.Sink, auditor auditRecorder) (mailflow.Consumer, error) {
	registration, ok := lookupDriverRegistration(sink.Driver)
	if !ok || registration.BuildLocalSink == nil {
		return nil, fmt.Errorf("sink driver %s 未提供本地 consumer 实现", sink.Driver)
	}
	return registration.BuildLocalSink(sink, cfg, auditor)
}

func ValidateSourceConfig(source appconfig.Source) error {
	registration, ok := lookupDriverRegistration(source.Driver)
	if !ok {
		return fmt.Errorf("source %s 不支持 driver: %s", source.Name, source.Driver)
	}
	if registration.ValidateSource != nil {
		return registration.ValidateSource(source)
	}
	return nil
}

func ValidateSinkConfig(sink appconfig.Sink) error {
	registration, ok := lookupDriverRegistration(sink.Driver)
	if !ok {
		return fmt.Errorf("sink %s 不支持 driver: %s", sink.Name, sink.Driver)
	}
	if registration.ValidateSink != nil {
		return registration.ValidateSink(sink)
	}
	return nil
}

func lookupDriverRegistration(driver string) (driverRegistration, bool) {
	registration, ok := driverRegistrations[normalizeDriver(driver)]
	return registration, ok
}

func normalizeDriver(driver string) string {
	return strings.ToLower(strings.TrimSpace(driver))
}
