package providers

import (
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

type auditRecorder interface {
	Record(event audit.Event) error
}

type localSinkDriver interface {
	provider.Driver
	BuildLocalSink(sink appconfig.Sink, cfg appconfig.Config, auditor auditRecorder) (mailflow.Consumer, error)
}

type loginConfigDriver interface {
	provider.Driver
	LoginConfig() *credentialRuntimeConfig
}

type revokeConfigDriver interface {
	provider.Driver
	RevokeConfig() *driverRevokeConfig
}

type backupDriver struct{}

func (backupDriver) Info() provider.DriverInfo {
	return provider.DriverInfo{
		Name: "backup",
		Sink: &provider.SinkCapabilities{
			RequiresOutputDir: true,
			LocalConsumer:     true,
			LocalConsumerKind: provider.LocalConsumerBackup,
		},
	}
}

func (backupDriver) BuildLocalSink(sink appconfig.Sink, _ appconfig.Config, auditor auditRecorder) (mailflow.Consumer, error) {
	return &adapters.BackupConsumer{
		OutputDir: sink.OutputDir,
		Auditor:   auditor,
	}, nil
}

type discardDriver struct{}

func (discardDriver) Info() provider.DriverInfo {
	return provider.DriverInfo{
		Name: "discard",
		Sink: &provider.SinkCapabilities{
			LocalConsumer:     true,
			LocalConsumerKind: provider.LocalConsumerDiscard,
		},
	}
}

func (discardDriver) BuildLocalSink(appconfig.Sink, appconfig.Config, auditRecorder) (mailflow.Consumer, error) {
	return &adapters.DiscardConsumer{}, nil
}

type fileDriver struct{}

func (fileDriver) Info() provider.DriverInfo {
	return provider.DriverInfo{
		Name: "file",
		Sink: &provider.SinkCapabilities{
			RequiresOutputDir: true,
			LocalConsumer:     true,
			LocalConsumerKind: provider.LocalConsumerFile,
		},
	}
}

func (fileDriver) BuildLocalSink(sink appconfig.Sink, _ appconfig.Config, _ auditRecorder) (mailflow.Consumer, error) {
	return &adapters.FileConsumer{OutputDir: sink.OutputDir}, nil
}

type ewsDriver struct{}

func (ewsDriver) Info() provider.DriverInfo {
	return provider.DriverInfo{
		Name: "ews",
		Auth: provider.AuthRequirement{
			Graph: true,
			EWS:   true,
		},
		Sink: &provider.SinkCapabilities{
			RequiresCredential: true,
			SupportsVerify:     true,
			SupportsReconcile:  true,
			SupportsHealth:     true,
		},
	}
}

func (ewsDriver) BuildSink(cfg appconfig.Config, folder string, tokenSource provider.TokenSource) (provider.SinkClients, error) {
	return graph.NewEWSWriterClients(cfg, folder, tokenSource)
}

func (ewsDriver) LoginConfig() *credentialRuntimeConfig { return microsoftDriverLoginConfig }
func (ewsDriver) RevokeConfig() *driverRevokeConfig     { return microsoftDriverRevokeConfig }

type graphDriver struct{}

func (graphDriver) Info() provider.DriverInfo {
	return provider.DriverInfo{
		Name: "graph",
		Auth: provider.AuthRequirement{
			Graph: true,
		},
		Source: &provider.SourceCapabilities{
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
		Sink: &provider.SinkCapabilities{
			RequiresCredential: true,
			SupportsVerify:     true,
			SupportsReconcile:  true,
			SupportsHealth:     true,
		},
	}
}

func (graphDriver) BuildSource(cfg appconfig.Config, folder string, tokenSource provider.TokenSource) (provider.SourceClients, error) {
	return graph.BuildSourceClientsWithTokenSource(cfg, folder, tokenSource)
}

func (graphDriver) BuildSourceRuntime(cfg appconfig.Config, source appconfig.Source, tokenSource provider.TokenSource, _ provider.SourceRuntimeOptions) (provider.SourceRuntime, error) {
	clients, err := graph.BuildSourceClientsWithTokenSource(cfg, source.Folder, tokenSource)
	if err != nil {
		return provider.SourceRuntime{}, err
	}
	return provider.SourceRuntime{Clients: clients}, nil
}

func (graphDriver) BuildSink(cfg appconfig.Config, folder string, tokenSource provider.TokenSource) (provider.SinkClients, error) {
	return graph.NewWriterClients(cfg, folder, tokenSource)
}

func (graphDriver) LoginConfig() *credentialRuntimeConfig { return microsoftDriverLoginConfig }
func (graphDriver) RevokeConfig() *driverRevokeConfig     { return microsoftDriverRevokeConfig }

type imapDriver struct{}

func (imapDriver) Info() provider.DriverInfo {
	return provider.DriverInfo{
		Name: "imap",
		Auth: provider.AuthRequirement{
			IMAP: true,
		},
		Source: &provider.SourceCapabilities{
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
		Sink: &provider.SinkCapabilities{
			RequiresCredential: true,
			SupportsVerify:     true,
			SupportsReconcile:  true,
			SupportsHealth:     true,
		},
	}
}

func (imapDriver) BuildSource(cfg appconfig.Config, folder string, tokenSource provider.TokenSource) (provider.SourceClients, error) {
	return imap.BuildSourceClientsWithTokenSource(cfg, folder, tokenSource)
}

func (imapDriver) BuildSourceRuntime(cfg appconfig.Config, source appconfig.Source, tokenSource provider.TokenSource, _ provider.SourceRuntimeOptions) (provider.SourceRuntime, error) {
	clients, err := imap.BuildSourceClientsWithTokenSource(cfg, source.Folder, tokenSource)
	if err != nil {
		return provider.SourceRuntime{}, err
	}
	return provider.SourceRuntime{Clients: clients}, nil
}

func (imapDriver) BuildSink(cfg appconfig.Config, folder string, tokenSource provider.TokenSource) (provider.SinkClients, error) {
	return imap.NewWriterClients(cfg, folder, tokenSource)
}

func (imapDriver) LoginConfig() *credentialRuntimeConfig { return microsoftDriverLoginConfig }
func (imapDriver) RevokeConfig() *driverRevokeConfig     { return microsoftDriverRevokeConfig }

type webhookDriver struct{}

func (webhookDriver) Info() provider.DriverInfo {
	return provider.DriverInfo{
		Name: "webhook",
		Source: &provider.SourceCapabilities{
			SupportsDelete:  false,
			DeleteSemantics: provider.DeleteSemanticsUnknown,
			Modes: map[string]provider.SourceModeSpec{
				"push": {},
			},
		},
	}
}

func (webhookDriver) BuildSourceRuntime(cfg appconfig.Config, source appconfig.Source, _ provider.TokenSource, options provider.SourceRuntimeOptions) (provider.SourceRuntime, error) {
	if options.EnqueuePushMessage == nil {
		return provider.SourceRuntime{}, nil
	}
	ingress, err := webhookdevice.BuildIngress(cfg, options.Route, source, options.EnqueuePushMessage)
	if err != nil {
		return provider.SourceRuntime{}, err
	}
	return provider.SourceRuntime{Ingress: ingress}, nil
}

func (webhookDriver) ConfigureSource(source appconfig.Source, in io.Reader, out io.Writer) (appconfig.Source, error) {
	return webhookdevice.ConfigureSource(source, in, out)
}

func (webhookDriver) DescribeSource(source appconfig.Source) []string {
	return webhookdevice.DescribeSource(source)
}

func (webhookDriver) ValidateSource(source appconfig.Source) error {
	return webhookdevice.ValidateSourceConfig(source)
}

var registeredDrivers = map[string]provider.Driver{
	"backup":  backupDriver{},
	"discard": discardDriver{},
	"ews":     ewsDriver{},
	"file":    fileDriver{},
	"graph":   graphDriver{},
	"imap":    imapDriver{},
	"webhook": webhookDriver{},
}

func LookupDriver(driver string) (provider.Driver, bool) {
	driverImpl, ok := registeredDrivers[normalizeDriver(driver)]
	return driverImpl, ok
}

func LookupDriverInfo(driver string) (provider.DriverInfo, bool) {
	driverImpl, ok := LookupDriver(driver)
	if !ok {
		return provider.DriverInfo{}, false
	}
	return driverImpl.Info(), true
}

func LookupDriverSpec(driver string) (provider.DriverSpec, bool) {
	return LookupDriverInfo(driver)
}

func LookupSourceInfo(driver string) (*provider.SourceCapabilities, bool) {
	info, ok := LookupDriverInfo(driver)
	if !ok || info.Source == nil {
		return nil, false
	}
	return info.Source, true
}

func LookupSourceSpec(driver string) (*provider.SourceSpec, bool) {
	return LookupSourceInfo(driver)
}

func LookupSinkInfo(driver string) (*provider.SinkCapabilities, bool) {
	info, ok := LookupDriverInfo(driver)
	if !ok || info.Sink == nil {
		return nil, false
	}
	return info.Sink, true
}

func LookupSinkSpec(driver string) (*provider.SinkSpec, bool) {
	return LookupSinkInfo(driver)
}

func AllDrivers() []provider.Driver {
	names := make([]string, 0, len(registeredDrivers))
	for name := range registeredDrivers {
		names = append(names, name)
	}
	sort.Strings(names)

	drivers := make([]provider.Driver, 0, len(names))
	for _, name := range names {
		drivers = append(drivers, registeredDrivers[name])
	}
	return drivers
}

func AllDriverInfos() []provider.DriverInfo {
	drivers := AllDrivers()
	infos := make([]provider.DriverInfo, 0, len(drivers))
	for _, driverImpl := range drivers {
		infos = append(infos, driverImpl.Info())
	}
	return infos
}

func AllDriverSpecs() []provider.DriverSpec {
	return AllDriverInfos()
}

func BuildLocalConsumer(cfg appconfig.Config, sink appconfig.Sink, auditor auditRecorder) (mailflow.Consumer, error) {
	driverImpl, ok := LookupDriver(sink.Driver)
	if !ok {
		return nil, fmt.Errorf("sink driver 未注册: %s", sink.Driver)
	}
	localDriver, ok := driverImpl.(localSinkDriver)
	if !ok {
		return nil, fmt.Errorf("sink driver %s 未提供本地 consumer 实现", sink.Driver)
	}
	return localDriver.BuildLocalSink(sink, cfg, auditor)
}

func ValidateSourceConfig(source appconfig.Source) error {
	driverImpl, ok := LookupDriver(source.Driver)
	if !ok {
		return fmt.Errorf("source %s 不支持 driver: %s", source.Name, source.Driver)
	}
	configurable, ok := driverImpl.(provider.SourceConfigurator)
	if !ok {
		return nil
	}
	return configurable.ValidateSource(source)
}

func ValidateSinkConfig(sink appconfig.Sink) error {
	driverImpl, ok := LookupDriver(sink.Driver)
	if !ok {
		return fmt.Errorf("sink %s 不支持 driver: %s", sink.Name, sink.Driver)
	}
	validator, ok := driverImpl.(provider.SinkValidator)
	if !ok {
		return nil
	}
	return validator.ValidateSink(sink)
}

func normalizeDriver(driver string) string {
	return strings.ToLower(strings.TrimSpace(driver))
}
