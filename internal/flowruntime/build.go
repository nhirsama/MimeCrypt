package flowruntime

import (
	"context"
	"fmt"
	"os"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/mailflow/adapters"
	"mimecrypt/internal/modules/audit"
	"mimecrypt/internal/modules/download"
	"mimecrypt/internal/modules/encrypt"
	"mimecrypt/internal/modules/health"
	"mimecrypt/internal/modules/list"
	"mimecrypt/internal/modules/writeback"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers"
)

type sourceBundle struct {
	Config  SourcePlan
	Clients provider.SourceClients
}

type sinkBundle struct {
	Config  SinkPlan
	Clients provider.SinkClients
}

func BuildDownloadService(plan SourcePlan) (*download.Service, error) {
	bundle, err := buildSourceBundle(plan)
	if err != nil {
		return nil, err
	}
	return &download.Service{Client: bundle.Clients.Reader}, nil
}

func BuildListService(plan SourcePlan) (*list.Service, error) {
	bundle, err := buildSourceBundle(plan)
	if err != nil {
		return nil, err
	}
	return &list.Service{Client: bundle.Clients.Reader}, nil
}

func BuildHealthService(ctx context.Context, run SourceRun) (*health.Service, error) {
	sourceSpec, ok := providers.LookupSourceSpec(run.Source.Driver)
	if !ok {
		return nil, fmt.Errorf("run 不支持 source driver=%s", run.Source.Driver)
	}

	service := &health.Service{
		StateDir:          run.Config.Auth.StateDir,
		Folder:            run.Source.Folder,
		Provider:          normalizeDriver(run.Source.Driver, "imap"),
		ProviderProbeKind: sourceSpec.ProbeKind,
	}
	service.SkipCachedToken = !sourceSpec.RequiresCredential
	service.SkipProviderProbe = sourceSpec.ProbeKind == ""

	if !service.SkipCachedToken || !service.SkipProviderProbe {
		source, err := buildSourceBundle(SourcePlan{
			Source: run.Source,
			Config: run.Config,
		})
		if err != nil {
			return nil, err
		}
		service.Reader = source.Clients.Reader
	}
	if !service.SkipCachedToken {
		session, err := buildTokenSource(run.Config, run.Source.Driver)
		if err != nil {
			return nil, err
		}
		tokenState, ok := session.(interface {
			LoadCachedToken() (provider.Token, error)
		})
		if !ok {
			return nil, fmt.Errorf("source driver %s 的 token source 不支持读取缓存 token", run.Source.Driver)
		}
		service.Session = tokenState
	}

	probes := make([]health.WriteBackProbe, 0, len(run.Route.Targets))
	seen := make(map[string]struct{}, len(run.Route.Targets))
	for _, target := range run.Route.Targets {
		sinkRef := strings.TrimSpace(target.SinkRef)
		if sinkRef == "" {
			continue
		}
		if _, exists := seen[sinkRef]; exists {
			continue
		}
		seen[sinkRef] = struct{}{}

		sink, ok := run.Sinks[sinkRef]
		if !ok {
			return nil, fmt.Errorf("route %s 引用了不存在的 sink: %s", run.Route.Name, sinkRef)
		}
		sinkSpec, ok := providers.LookupSinkSpec(sink.Sink.Driver)
		if !ok {
			return nil, fmt.Errorf("route %s 引用了不支持的 sink driver: %s", run.Route.Name, sink.Sink.Driver)
		}
		if !sinkSpec.SupportsHealth {
			continue
		}

		sinkBundle, err := buildSinkBundle(sink)
		if err != nil {
			return nil, err
		}
		probes = append(probes, health.WriteBackProbe{
			Name:   sink.Sink.Name,
			Driver: sink.Sink.Driver,
			Health: sinkBundle.Clients.Health,
		})
	}

	if len(probes) == 1 {
		service.WriteBackProvider = normalizeDriver(probes[0].Driver, "")
		service.WriteBack = probes[0].Health
	} else {
		service.WriteBacks = probes
	}
	return service, nil
}

func buildEnvelopeBuilderFromSourceBundle(ctx context.Context, run SourceRun, source sourceBundle) (*adapters.ReaderEnvelopeBuilder, error) {
	sourceStore, err := buildMailflowSourceStore(ctx, run.Config, source.Clients.Reader, run.Source, run.Route.DeleteSource.Enabled)
	if err != nil {
		return nil, err
	}
	return &adapters.ReaderEnvelopeBuilder{
		Name:            run.Source.Name,
		Driver:          normalizeDriver(run.Source.Driver, "imap"),
		Folder:          run.Source.Folder,
		Store:           sourceStore,
		Reader:          source.Clients.Reader,
		Deleter:         source.Clients.Deleter,
		DeleteSemantics: run.SourceDeleteSemantics,
	}, nil
}

func BuildRunner(ctx context.Context, run SourceRun) (*mailflow.Runner, error) {
	sourceSpec, ok := providers.LookupSourceSpec(run.Source.Driver)
	if !ok {
		return nil, fmt.Errorf("run 不支持 source driver=%s", run.Source.Driver)
	}
	mode := strings.TrimSpace(run.Source.Mode)
	if _, ok := sourceSpec.ModeSpec(mode); !ok {
		return nil, fmt.Errorf("run 不支持 source=%s driver=%s mode=%s", run.Source.Name, run.Source.Driver, mode)
	}
	if !strings.EqualFold(mode, "poll") {
		return nil, fmt.Errorf("run 尚未实现 source=%s 的 mode=%s", run.Source.Name, mode)
	}
	source, err := buildSourceBundle(SourcePlan{
		Source: run.Source,
		Config: run.Config,
	})
	if err != nil {
		return nil, err
	}
	coordinator, err := buildCoordinatorForMode(ctx, run, TransactionModePersistent)
	if err != nil {
		return nil, err
	}
	sourceStore, err := buildMailflowSourceStore(ctx, run.Config, source.Clients.Reader, run.Source, run.Route.DeleteSource.Enabled)
	if err != nil {
		return nil, err
	}

	return &mailflow.Runner{
		Producer: &adapters.PollingProducer{
			Name:            run.Source.Name,
			Driver:          run.Source.Driver,
			Folder:          run.Source.Folder,
			StatePath:       run.Source.StatePath,
			IncludeExisting: run.Source.IncludeExisting,
			Store:           sourceStore,
			Reader:          source.Clients.Reader,
			Deleter:         source.Clients.Deleter,
			DeleteSemantics: run.SourceDeleteSemantics,
		},
		Coordinator: coordinator,
	}, nil
}

func buildCoordinatorWithStore(ctx context.Context, run SourceRun, store mailflow.StateStore) (*mailflow.Coordinator, error) {
	plan, err := buildMailflowPlan(run.Route)
	if err != nil {
		return nil, err
	}

	backupEncryptor, err := buildCatchAllBackupEncryptor(run.Config)
	if err != nil {
		return nil, err
	}

	auditor := &audit.Service{
		Path:   run.Config.Mail.Pipeline.AuditLogPath,
		Stdout: run.Config.Mail.Pipeline.AuditStdout,
		Writer: os.Stdout,
	}

	consumers, err := buildMailflowConsumers(ctx, run, auditor)
	if err != nil {
		return nil, err
	}
	if store == nil {
		store = mailflow.FileStateStore{Dir: run.Route.StateDir}
	}

	return &mailflow.Coordinator{
		Processor: &adapters.EncryptingProcessor{
			Encryptor:       &encrypt.Service{ProtectSubject: run.Config.Mail.Pipeline.ProtectSubject},
			BackupEncryptor: backupEncryptor,
			Auditor:         auditor,
			WorkDir:         run.Config.Mail.Pipeline.WorkDir,
			StaticPlan:      plan,
		},
		Store:     store,
		Consumers: consumers,
	}, nil
}

func buildMailflowConsumers(ctx context.Context, run SourceRun, auditor *audit.Service) (map[string]mailflow.Consumer, error) {
	consumers := make(map[string]mailflow.Consumer)
	for _, target := range run.Route.Targets {
		sinkRef := strings.TrimSpace(target.SinkRef)
		if sinkRef == "" {
			continue
		}
		if _, exists := consumers[sinkRef]; exists {
			continue
		}

		sink, ok := run.Sinks[sinkRef]
		if !ok {
			return nil, fmt.Errorf("route %s 引用了不存在的 sink: %s", run.Route.Name, sinkRef)
		}

		sinkSpec, ok := providers.LookupSinkSpec(sink.Sink.Driver)
		if !ok {
			return nil, fmt.Errorf("route %s 引用了不支持的 sink driver: %s", run.Route.Name, sink.Sink.Driver)
		}

		if sinkSpec.LocalConsumer {
			consumer, err := providers.BuildLocalConsumer(sink.Config, sink.Sink, auditor)
			if err != nil {
				return nil, err
			}
			consumers[sinkRef] = consumer
			continue
		}

		sinkBundle, err := buildSinkBundle(sink)
		if err != nil {
			return nil, err
		}
		sinkStore, err := buildMailflowSinkStore(ctx, sink.Config, sinkBundle.Clients.Reader, sink.Sink.Driver, sink.Mailbox, run.Route.DeleteSource.Enabled)
		if err != nil {
			return nil, err
		}
		consumers[sinkRef] = &adapters.WritebackConsumer{
			Service: &writeback.Service{
				Writer:     sinkBundle.Clients.Writer,
				Reconciler: sinkBundle.Clients.Reconciler,
			},
			DestinationFolderID: sink.Mailbox,
			Verify:              sink.Sink.Verify,
			Store:               sinkStore,
		}
	}
	return consumers, nil
}

func buildMailflowPlan(route appconfig.Route) (mailflow.ExecutionPlan, error) {
	targets := make([]mailflow.DeliveryTarget, 0, len(route.Targets))
	for _, target := range route.Targets {
		artifact := strings.TrimSpace(target.Artifact)
		if artifact == "" {
			artifact = "primary"
		}
		targets = append(targets, mailflow.DeliveryTarget{
			Name:     strings.TrimSpace(target.Name),
			Consumer: strings.TrimSpace(target.SinkRef),
			Artifact: artifact,
			Required: target.Required,
		})
	}

	plan := mailflow.ExecutionPlan{Targets: targets}
	if route.DeleteSource.Enabled {
		plan.DeleteSource = mailflow.DeleteSourcePolicy{
			Enabled:           true,
			RequireSameStore:  route.DeleteSource.RequireSameStore,
			EligibleConsumers: append([]string(nil), route.DeleteSource.EligibleSinks...),
		}
	}
	if err := plan.Validate(); err != nil {
		return mailflow.ExecutionPlan{}, err
	}
	return plan, nil
}

func buildSourceBundle(plan SourcePlan) (sourceBundle, error) {
	tokenSource, err := buildTokenSource(plan.Config, plan.Source.Driver)
	if err != nil {
		return sourceBundle{}, err
	}
	clients, err := providers.BuildSourceClients(plan.Config, plan.Source.Driver, plan.Source.Folder, tokenSource)
	if err != nil {
		return sourceBundle{}, err
	}
	return sourceBundle{
		Config:  plan,
		Clients: clients,
	}, nil
}

func buildSinkBundle(plan SinkPlan) (sinkBundle, error) {
	tokenSource, err := buildTokenSource(plan.Config, plan.Sink.Driver)
	if err != nil {
		return sinkBundle{}, err
	}
	clients, err := providers.BuildSinkClients(plan.Config, plan.Sink.Driver, plan.Mailbox, tokenSource)
	if err != nil {
		return sinkBundle{}, err
	}
	return sinkBundle{
		Config:  plan,
		Clients: clients,
	}, nil
}

func buildMailflowSourceStore(ctx context.Context, cfg appconfig.Config, reader provider.Reader, source appconfig.Source, resolveAccount bool) (mailflow.StoreRef, error) {
	driver := normalizeDriver(source.Driver, "imap")
	account := ""
	var err error
	if resolveAccount {
		account, err = resolveStoreAccount(ctx, driver, cfg, reader)
		if err != nil {
			return mailflow.StoreRef{}, err
		}
	}
	return mailflow.StoreRef{
		Driver:  driver,
		Account: account,
		Mailbox: source.Folder,
	}, nil
}

func buildMailflowSinkStore(ctx context.Context, cfg appconfig.Config, reader provider.Reader, sinkDriver, mailbox string, resolveAccount bool) (mailflow.StoreRef, error) {
	driver := normalizeDriver(sinkDriver, "imap")
	account := ""
	var err error
	if resolveAccount {
		account, err = resolveStoreAccount(ctx, driver, cfg, reader)
		if err != nil {
			return mailflow.StoreRef{}, err
		}
	}
	return mailflow.StoreRef{
		Driver:  driver,
		Account: account,
		Mailbox: strings.TrimSpace(mailbox),
	}, nil
}

func resolveStoreAccount(ctx context.Context, driver string, cfg appconfig.Config, reader provider.Reader) (string, error) {
	driver = normalizeDriver(driver, "")
	if sourceSpec, ok := providers.LookupSourceSpec(driver); ok && sourceSpec.ProbeKind == provider.ProviderProbeFolderList {
		return strings.TrimSpace(cfg.Mail.Client.IMAPUsername), nil
	}
	if reader == nil {
		return "", nil
	}
	user, err := reader.Me(ctx)
	if err != nil {
		return "", fmt.Errorf("查询当前邮箱账号失败: %w", err)
	}
	if account := strings.TrimSpace(user.Account()); account != "" {
		return account, nil
	}
	return strings.TrimSpace(user.ID), nil
}

func buildTokenSource(cfg appconfig.Config, driver string) (provider.TokenSource, error) {
	spec, ok := providers.LookupDriverSpec(driver)
	if !ok {
		return nil, fmt.Errorf("不支持的 driver: %s", driver)
	}
	needsCredential := (spec.Source != nil && spec.Source.RequiresCredential) || (spec.Sink != nil && spec.Sink.RequiresCredential)
	if !needsCredential {
		return nil, nil
	}
	authCfg := providers.SessionAuthConfigForDrivers(cfg, driver)
	session, err := auth.NewSession(authCfg, nil)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func sourceDeleteSemantics(driver string) provider.DeleteSemantics {
	sourceSpec, ok := providers.LookupSourceSpec(driver)
	if !ok {
		return provider.DeleteSemanticsUnknown
	}
	return sourceSpec.DeleteSemantics
}

func buildCatchAllBackupEncryptor(cfg appconfig.Config) (*encrypt.Service, error) {
	key := strings.TrimSpace(cfg.Mail.Pipeline.BackupKeyID)
	if key == "" {
		return nil, nil
	}
	return &encrypt.Service{
		RecipientResolver: func([]byte) ([]string, error) {
			return []string{key}, nil
		},
	}, nil
}
