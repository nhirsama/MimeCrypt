package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/mailflow/adapters"
	"mimecrypt/internal/modules/audit"
	"mimecrypt/internal/modules/backup"
	"mimecrypt/internal/modules/discover"
	"mimecrypt/internal/modules/download"
	"mimecrypt/internal/modules/encrypt"
	"mimecrypt/internal/modules/health"
	"mimecrypt/internal/modules/list"
	"mimecrypt/internal/modules/login"
	"mimecrypt/internal/modules/logout"
	"mimecrypt/internal/modules/process"
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

	return &list.Service{Client: clients.Reader}, nil
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

func buildProcessService(cfg appconfig.Config) (*process.Service, error) {
	clients, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	return buildProcessServiceWithProvider(cfg, clients.Reader, clients.Writer)
}

func buildDownloadServiceWithReader(reader provider.Reader) *download.Service {
	return &download.Service{Client: reader}
}

func buildProcessServiceWithProvider(cfg appconfig.Config, reader provider.Reader, writer provider.Writer) (*process.Service, error) {
	backupEncryptor, err := buildCatchAllBackupEncryptor(cfg)
	if err != nil {
		return nil, err
	}

	return &process.Service{
		Downloader:      buildDownloadServiceWithReader(reader),
		Encryptor:       &encrypt.Service{ProtectSubject: cfg.Mail.Pipeline.ProtectSubject},
		BackupEncryptor: backupEncryptor,
		Backupper:       &backup.Service{},
		WriteBack:       &writeback.Service{Writer: writer},
		Auditor: &audit.Service{
			Path:   cfg.Mail.Pipeline.AuditLogPath,
			Stdout: cfg.Mail.Pipeline.AuditStdout,
			Writer: os.Stdout,
		},
	}, nil
}

func buildProcessRequest(cfg appconfig.Config, source provider.MessageRef, writeBack bool, writeBackFolder string, verifyWriteBack bool) process.Request {
	return process.Request{
		Source:     source,
		OutputDir:  cfg.Mail.Pipeline.OutputDir,
		SaveOutput: cfg.Mail.Pipeline.SaveOutput,
		WorkDir:    cfg.Mail.Pipeline.WorkDir,
		BackupDir:  cfg.Mail.Pipeline.BackupDir,
		WriteBack: process.WriteBackOptions{
			Enabled:             writeBack,
			DestinationFolderID: writeBackFolder,
			Verify:              verifyWriteBack,
		},
	}
}

func buildDiscoverService(cfg appconfig.Config) (*discover.Service, error) {
	clients, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	processor, err := buildProcessServiceWithProvider(cfg, clients.Reader, clients.Writer)
	if err != nil {
		return nil, err
	}

	return &discover.Service{
		Client:    clients.Reader,
		Processor: processor,
	}, nil
}

func buildMailflowRunner(ctx context.Context, cfg appconfig.Config, includeExisting, writeBack, verifyWriteBack, deleteSource bool) (*mailflow.Runner, error) {
	clients, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	plan, err := buildMailflowPlan(cfg.Mail.Pipeline.SaveOutput, writeBack, deleteSource)
	if err != nil {
		return nil, err
	}

	backupEncryptor, err := buildCatchAllBackupEncryptor(cfg)
	if err != nil {
		return nil, err
	}

	sourceDriver := normalizeDriver(cfg.Provider, "imap")
	sourceAccount := ""
	if deleteSource {
		sourceAccount, err = resolveStoreAccount(ctx, sourceDriver, cfg, clients.Reader)
		if err != nil {
			return nil, err
		}
	}
	sourceStore := mailflow.StoreRef{
		Driver:  sourceDriver,
		Account: sourceAccount,
		Mailbox: cfg.Mail.Sync.Folder,
	}

	consumers := make(map[string]mailflow.Consumer)
	if cfg.Mail.Pipeline.SaveOutput {
		consumers["local-output"] = &adapters.FileConsumer{
			OutputDir: cfg.Mail.Pipeline.OutputDir,
		}
	}
	if writeBack {
		sinkDriver := normalizeDriver(cfg.Mail.Pipeline.WriteBackProvider, "imap")
		sinkAccount := ""
		if deleteSource {
			sinkAccount, err = resolveStoreAccount(ctx, sinkDriver, cfg, clients.Reader)
			if err != nil {
				return nil, err
			}
		}
		destinationFolder := strings.TrimSpace(cfg.Mail.Pipeline.WriteBackFolder)
		if destinationFolder == "" {
			destinationFolder = cfg.Mail.Sync.Folder
		}
		consumers["write-back"] = &adapters.WritebackConsumer{
			Service:             &writeback.Service{Writer: clients.Writer},
			DestinationFolderID: cfg.Mail.Pipeline.WriteBackFolder,
			Verify:              verifyWriteBack,
			Store: mailflow.StoreRef{
				Driver:  sinkDriver,
				Account: sinkAccount,
				Mailbox: destinationFolder,
			},
		}
	}

	return &mailflow.Runner{
		Producer: &adapters.PollingProducer{
			Name:            "default",
			Driver:          sourceDriver,
			Folder:          cfg.Mail.Sync.Folder,
			StatePath:       cfg.Mail.FlowProducerStatePath(),
			IncludeExisting: includeExisting,
			Store:           sourceStore,
			Reader:          clients.Reader,
			Deleter:         clients.Deleter,
		},
		Coordinator: &mailflow.Coordinator{
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
			Store: mailflow.FileStateStore{
				Dir: cfg.Mail.FlowStateDir(),
			},
			Consumers: consumers,
		},
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
	if !saveOutput && !writeBack {
		return fmt.Errorf("至少需要一个消费目标：--save-output 或 --write-back")
	}
	return nil
}

func buildMailflowPlan(saveOutput, writeBack, deleteSource bool) (mailflow.ExecutionPlan, error) {
	targets := make([]mailflow.DeliveryTarget, 0, 2)
	if saveOutput {
		targets = append(targets, mailflow.DeliveryTarget{
			Name:     "local-output",
			Consumer: "local-output",
			Artifact: "primary",
			Required: true,
		})
	}
	if writeBack {
		targets = append(targets, mailflow.DeliveryTarget{
			Name:     "write-back",
			Consumer: "write-back",
			Artifact: "primary",
			Required: true,
		})
	}

	plan := mailflow.ExecutionPlan{
		Targets: targets,
	}
	if deleteSource {
		plan.DeleteSource = mailflow.DeleteSourcePolicy{
			Enabled:           true,
			RequireSameStore:  true,
			EligibleConsumers: []string{"write-back"},
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

func buildCatchAllBackupEncryptor(cfg appconfig.Config) (*encrypt.Service, error) {
	if strings.TrimSpace(cfg.Mail.Pipeline.BackupKeyID) == "" {
		return nil, nil
	}
	return buildLocalEncryptService(nil, []string{cfg.Mail.Pipeline.BackupKeyID}, "", false)
}
