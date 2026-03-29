package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/mailflow/adapters"
	"mimecrypt/internal/modules/audit"
	"mimecrypt/internal/modules/backup"
	"mimecrypt/internal/modules/download"
	"mimecrypt/internal/modules/encrypt"
	"mimecrypt/internal/modules/health"
	"mimecrypt/internal/modules/list"
	"mimecrypt/internal/modules/login"
	"mimecrypt/internal/modules/logout"
	"mimecrypt/internal/modules/tokenstate"
	"mimecrypt/internal/modules/writeback"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers"
)

func newErrorCommand(use, short string, err error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(*cobra.Command, []string) error {
			return err
		},
	}
}

func buildProviderClients(cfg appconfig.Config) (provider.Clients, error) {
	return providers.Build(cfg)
}

func buildLoginService(cfg appconfig.Config) (*login.Service, error) {
	clients, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	return &login.Service{
		Session:  clients.Session,
		Mail:     clients.Reader,
		StateDir: cfg.Auth.StateDir,
	}, nil
}

func buildLogoutService(cfg appconfig.Config) (*logout.Service, error) {
	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		return nil, err
	}
	return &logout.Service{Session: session}, nil
}

func buildDownloadService(cfg appconfig.Config) (*download.Service, error) {
	clients, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	return buildDownloadServiceWithReader(clients.Reader), nil
}

func buildListService(cfg appconfig.Config) (*list.Service, error) {
	clients, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	return buildListServiceWithReader(clients.Reader), nil
}

func buildHealthService(cfg appconfig.Config) (*health.Service, error) {
	clients, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	return &health.Service{
		StateDir:          cfg.Auth.StateDir,
		Folder:            cfg.Mail.Sync.Folder,
		Provider:          cfg.Provider,
		WriteBackProvider: cfg.Mail.Pipeline.WriteBackProvider,
		Session:           clients.Session,
		Reader:            clients.Reader,
		Writer:            clients.Writer,
	}, nil
}

func buildTopologyHealthService(ctx context.Context, cfg appconfig.Config, resolved resolvedMailflowTopology) (*health.Service, error) {
	sourceClients, err := buildSourceProviderClients(cfg, resolved.Source)
	if err != nil {
		return nil, err
	}

	service := &health.Service{
		StateDir: cfg.Auth.StateDir,
		Folder:   resolved.Source.Folder,
		Provider: normalizeDriver(resolved.Source.Driver, cfg.Provider),
		Session:  sourceClients.Session,
		Reader:   sourceClients.Reader,
	}

	probes := make([]health.WriteBackProbe, 0, len(resolved.Route.Targets))
	seen := make(map[string]struct{}, len(resolved.Route.Targets))
	for _, target := range resolved.Route.Targets {
		sinkRef := strings.TrimSpace(target.SinkRef)
		if sinkRef == "" {
			continue
		}
		if _, exists := seen[sinkRef]; exists {
			continue
		}
		seen[sinkRef] = struct{}{}

		sink, ok := resolved.Topology.Sinks[sinkRef]
		if !ok {
			return nil, fmt.Errorf("route %s 引用了不存在的 sink: %s", resolved.Route.Name, sinkRef)
		}
		switch normalizeDriver(sink.Driver, "") {
		case "file", "discard":
			continue
		}

		sinkClients, err := buildSinkProviderClients(cfg, resolved.Source, sink)
		if err != nil {
			return nil, err
		}
		probes = append(probes, health.WriteBackProbe{
			Name:   sink.Name,
			Driver: sink.Driver,
			Writer: sinkClients.Writer,
		})
	}

	if len(probes) == 1 {
		service.WriteBackProvider = normalizeDriver(probes[0].Driver, "")
		service.Writer = probes[0].Writer
	} else {
		service.WriteBacks = probes
	}
	return service, nil
}

func buildTokenStateService(cfg appconfig.Config) (*tokenstate.Service, error) {
	session, err := auth.NewSession(cfg.Auth, nil)
	if err != nil {
		return nil, err
	}

	return &tokenstate.Service{
		Session:        session,
		StateDir:       cfg.Auth.StateDir,
		TokenStore:     cfg.Auth.TokenStoreMode(),
		KeyringService: cfg.Auth.KeyringServiceName(),
	}, nil
}

func buildDownloadServiceWithReader(reader provider.Reader) *download.Service {
	return &download.Service{Client: reader}
}

func buildListServiceWithReader(reader provider.Reader) *list.Service {
	return &list.Service{Client: reader}
}

func buildMailflowRunner(ctx context.Context, cfg appconfig.Config, resolved resolvedMailflowTopology) (*mailflow.Runner, error) {
	source := resolved.Source
	if !strings.EqualFold(source.Mode, "poll") {
		return nil, fmt.Errorf("run 仅支持 polling source，当前 source=%s mode=%s", source.Name, source.Mode)
	}
	sourceClients, err := buildSourceProviderClients(cfg, source)
	if err != nil {
		return nil, err
	}
	coordinator, err := buildMailflowCoordinator(ctx, cfg, resolved)
	if err != nil {
		return nil, err
	}
	sourceStore, err := buildMailflowSourceStore(ctx, cfg, sourceClients.Reader, source, resolved.Route.DeleteSource.Enabled)
	if err != nil {
		return nil, err
	}

	return &mailflow.Runner{
		Producer: &adapters.PollingProducer{
			Name:            source.Name,
			Driver:          source.Driver,
			Folder:          source.Folder,
			StatePath:       source.StatePath,
			IncludeExisting: source.IncludeExisting,
			Store:           sourceStore,
			Reader:          sourceClients.Reader,
			Deleter:         sourceClients.Deleter,
		},
		Coordinator: coordinator,
	}, nil
}

func buildMailflowCoordinator(ctx context.Context, cfg appconfig.Config, resolved resolvedMailflowTopology) (*mailflow.Coordinator, error) {
	return buildMailflowCoordinatorWithStore(ctx, cfg, resolved, nil)
}

func buildMailflowCoordinatorWithStore(ctx context.Context, cfg appconfig.Config, resolved resolvedMailflowTopology, store mailflow.StateStore) (*mailflow.Coordinator, error) {
	route := resolved.Route
	plan, err := buildMailflowPlan(route)
	if err != nil {
		return nil, err
	}

	backupEncryptor, err := buildCatchAllBackupEncryptor(cfg)
	if err != nil {
		return nil, err
	}

	consumers, err := buildMailflowConsumers(ctx, cfg, resolved.Topology, resolved.Source, route)
	if err != nil {
		return nil, err
	}
	if store == nil {
		store = mailflow.FileStateStore{Dir: route.StateDir}
	}

	return &mailflow.Coordinator{
		Processor: &adapters.EncryptingProcessor{
			Encryptor:       &encrypt.Service{ProtectSubject: cfg.Mail.Pipeline.ProtectSubject},
			BackupEncryptor: backupEncryptor,
			Backupper:       &backup.Service{},
			Auditor: &audit.Service{
				Path:   cfg.Mail.Pipeline.AuditLogPath,
				Stdout: cfg.Mail.Pipeline.AuditStdout,
				Writer: os.Stdout,
			},
			WorkDir:    cfg.Mail.Pipeline.WorkDir,
			BackupDir:  cfg.Mail.Pipeline.BackupDir,
			StaticPlan: plan,
		},
		Store:     store,
		Consumers: consumers,
	}, nil
}

func buildMailflowConsumers(ctx context.Context, cfg appconfig.Config, topology appconfig.Topology, source appconfig.Source, route appconfig.Route) (map[string]mailflow.Consumer, error) {
	consumers := make(map[string]mailflow.Consumer)
	for _, target := range route.Targets {
		sinkRef := strings.TrimSpace(target.SinkRef)
		if sinkRef == "" {
			continue
		}
		if _, exists := consumers[sinkRef]; exists {
			continue
		}
		sink, ok := topology.Sinks[sinkRef]
		if !ok {
			return nil, fmt.Errorf("route %s 引用了不存在的 sink: %s", route.Name, sinkRef)
		}
		switch strings.ToLower(strings.TrimSpace(sink.Driver)) {
		case "discard":
			consumers[sinkRef] = &adapters.DiscardConsumer{}
		case "file":
			consumers[sinkRef] = &adapters.FileConsumer{
				OutputDir: sink.OutputDir,
			}
		default:
			sinkClients, err := buildSinkProviderClients(cfg, source, sink)
			if err != nil {
				return nil, err
			}
			sinkStore, err := buildMailflowSinkStore(ctx, cfg, sinkClients.Reader, sink, source.Folder, route.DeleteSource.Enabled)
			if err != nil {
				return nil, err
			}
			consumers[sinkRef] = &adapters.WritebackConsumer{
				Service:             &writeback.Service{Writer: sinkClients.Writer},
				DestinationFolderID: sink.Folder,
				Verify:              sink.Verify,
				Store:               sinkStore,
			}
		}
	}
	return consumers, nil
}

func buildSourceProviderClients(cfg appconfig.Config, source appconfig.Source) (provider.Clients, error) {
	sourceCfg := cfg
	sourceCfg.Provider = normalizeDriver(source.Driver, "imap")
	sourceCfg.Mail.Sync.Folder = source.Folder
	return providers.BuildSourceClients(sourceCfg)
}

func buildSinkProviderClients(cfg appconfig.Config, source appconfig.Source, sink appconfig.Sink) (provider.Clients, error) {
	sinkCfg := cfg
	sinkDriver := normalizeDriver(sink.Driver, "imap")
	sinkCfg.Provider = sourceProviderForSinkDriver(sinkDriver)
	sinkCfg.Mail.Pipeline.WriteBackProvider = sinkDriver
	sinkCfg.Mail.Sync.Folder = source.Folder
	if folder := strings.TrimSpace(sink.Folder); folder != "" {
		sinkCfg.Mail.Sync.Folder = folder
	}
	return buildProviderClients(sinkCfg)
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

func buildMailflowSinkStore(ctx context.Context, cfg appconfig.Config, reader provider.Reader, sink appconfig.Sink, fallbackMailbox string, resolveAccount bool) (mailflow.StoreRef, error) {
	driver := normalizeDriver(sink.Driver, "imap")
	account := ""
	var err error
	if resolveAccount {
		account, err = resolveStoreAccount(ctx, driver, cfg, reader)
		if err != nil {
			return mailflow.StoreRef{}, err
		}
	}
	mailbox := strings.TrimSpace(sink.Folder)
	if mailbox == "" {
		mailbox = strings.TrimSpace(fallbackMailbox)
	}
	if mailbox == "" {
		mailbox = cfg.Mail.Sync.Folder
	}
	return mailflow.StoreRef{
		Driver:  driver,
		Account: account,
		Mailbox: mailbox,
	}, nil
}

func syncConfig(defaults appconfig.Config, clientID, tenant, stateDir, authorityBaseURL, graphBaseURL, ewsBaseURL, imapAddr, imapUsername string) appconfig.Config {
	cfg := defaults
	previousStateDir := cfg.Mail.Sync.StateDir
	previousAuditLogPath := cfg.Mail.Pipeline.AuditLogPath
	cfg.Auth.ClientID = clientID
	cfg.Auth.Tenant = tenant
	cfg.Auth.StateDir = stateDir
	cfg.Auth.AuthorityBaseURL = authorityBaseURL
	cfg.Mail.Client.GraphBaseURL = graphBaseURL
	cfg.Mail.Client.EWSBaseURL = ewsBaseURL
	cfg.Mail.Client.IMAPAddr = imapAddr
	cfg.Mail.Client.IMAPUsername = imapUsername
	cfg.Mail.Sync.StateDir = stateDir
	if strings.TrimSpace(previousAuditLogPath) == "" || previousAuditLogPath == appconfig.DefaultAuditLogPath(previousStateDir) {
		cfg.Mail.Pipeline.AuditLogPath = appconfig.DefaultAuditLogPath(stateDir)
	} else if !filepath.IsAbs(previousAuditLogPath) && previousAuditLogPath == filepath.Base(appconfig.DefaultAuditLogPath(previousStateDir)) {
		cfg.Mail.Pipeline.AuditLogPath = filepath.Join(stateDir, previousAuditLogPath)
	}
	return cfg
}

func validateWriteBackFlags(writeBack, verifyWriteBack bool, writeBackFolder string) error {
	if verifyWriteBack && !writeBack {
		return fmt.Errorf("--verify-write-back 依赖 --write-back")
	}
	if strings.TrimSpace(writeBackFolder) != "" && !writeBack {
		return fmt.Errorf("--write-back-folder 依赖 --write-back")
	}

	return nil
}

func validateMailflowFlags(saveOutput, writeBack, verifyWriteBack, deleteSource bool, writeBackFolder string) error {
	if err := validateWriteBackFlags(writeBack, verifyWriteBack, writeBackFolder); err != nil {
		return err
	}
	if deleteSource && !writeBack {
		return fmt.Errorf("--delete-source 依赖 --write-back")
	}
	return nil
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
			Options:  target.Options,
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

func resolveStoreAccount(ctx context.Context, driver string, cfg appconfig.Config, reader provider.Reader) (string, error) {
	driver = normalizeDriver(driver, "")
	if driver == "imap" {
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

func normalizeDriver(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return strings.ToLower(strings.TrimSpace(fallback))
	}
	return value
}

func sourceProviderForSinkDriver(driver string) string {
	switch normalizeDriver(driver, "imap") {
	case "ews":
		return "graph"
	default:
		return normalizeDriver(driver, "imap")
	}
}

type mailflowSummary struct {
	MessageID        string
	Format           string
	Encrypted        bool
	AlreadyEncrypted bool
	SavedOutput      bool
	BackupPath       string
	WroteBack        bool
	Verified         bool
	Path             string
	Bytes            int64
}

func summarizeMailflowResult(result mailflow.Result) (mailflowSummary, error) {
	summary := mailflowSummary{
		MessageID:  strings.TrimSpace(result.Trace.SourceMessageID),
		Format:     strings.TrimSpace(result.Trace.Attributes["format"]),
		BackupPath: strings.TrimSpace(result.Trace.Attributes["backup_path"]),
	}
	if summary.MessageID == "" {
		summary.MessageID = strings.TrimSpace(result.Key)
	}
	if result.Skipped {
		summary.AlreadyEncrypted = result.Trace.Attributes["already_encrypted"] == "true"
		return summary, nil
	}
	summary.Encrypted = true

	for _, receipt := range result.Deliveries {
		switch strings.TrimSpace(receipt.Consumer) {
		case "local-output":
			summary.SavedOutput = true
			summary.Path = receipt.ID
			if summary.Path != "" {
				info, err := os.Stat(summary.Path)
				if err != nil {
					return mailflowSummary{}, fmt.Errorf("读取输出文件信息失败: %w", err)
				}
				summary.Bytes = info.Size()
			}
		case "write-back":
			summary.WroteBack = true
			summary.Verified = summary.Verified || receipt.Verified
		}
	}

	if summary.Bytes == 0 && result.Trace.Attributes != nil {
		if value := strings.TrimSpace(result.Trace.Attributes["output_bytes"]); value != "" {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return mailflowSummary{}, fmt.Errorf("解析 output bytes 失败: %w", err)
			}
			summary.Bytes = parsed
		}
	}

	return summary, nil
}

func buildCatchAllBackupEncryptor(cfg appconfig.Config) (*encrypt.Service, error) {
	if strings.TrimSpace(cfg.Mail.Pipeline.BackupKeyID) == "" {
		return nil, nil
	}
	return buildLocalEncryptService(nil, []string{cfg.Mail.Pipeline.BackupKeyID}, "", false)
}
