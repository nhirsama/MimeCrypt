package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/modules/audit"
	"mimecrypt/internal/modules/backup"
	"mimecrypt/internal/modules/discover"
	"mimecrypt/internal/modules/download"
	"mimecrypt/internal/modules/encrypt"
	"mimecrypt/internal/modules/login"
	"mimecrypt/internal/modules/logout"
	"mimecrypt/internal/modules/process"
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

func buildProviderClients(cfg appconfig.Config) (provider.Session, provider.Reader, provider.Writer, error) {
	return providers.Build(cfg)
}

func buildLoginService(cfg appconfig.Config) (*login.Service, error) {
	session, reader, _, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	return &login.Service{
		Session:  session,
		Mail:     reader,
		StateDir: cfg.Auth.StateDir,
	}, nil
}

func buildLogoutService(cfg appconfig.Config) *logout.Service {
	return &logout.Service{TokenPath: cfg.Auth.TokenPath()}
}

func buildDownloadService(cfg appconfig.Config) (*download.Service, error) {
	_, reader, _, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	return buildDownloadServiceWithReader(reader), nil
}

func buildProcessService(cfg appconfig.Config) (*process.Service, error) {
	_, reader, writer, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	return buildProcessServiceWithProvider(cfg, reader, writer), nil
}

func buildDownloadServiceWithReader(reader provider.Reader) *download.Service {
	return &download.Service{Client: reader}
}

func buildProcessServiceWithProvider(cfg appconfig.Config, reader provider.Reader, writer provider.Writer) *process.Service {
	return &process.Service{
		Downloader:      buildDownloadServiceWithReader(reader),
		Encryptor:       &encrypt.Service{},
		BackupEncryptor: buildCatchAllBackupEncryptor(cfg),
		Backupper:       &backup.Service{},
		WriteBack:       &writeback.Service{Writer: writer},
		Auditor:         &audit.Service{Path: cfg.Mail.Pipeline.AuditLogPath},
	}
}

func buildProcessRequest(cfg appconfig.Config, source provider.MessageRef, writeBack bool, writeBackFolder string, verifyWriteBack bool) process.Request {
	return process.Request{
		Source:     source,
		OutputDir:  cfg.Mail.Pipeline.OutputDir,
		SaveOutput: cfg.Mail.Pipeline.SaveOutput,
		BackupDir:  cfg.Mail.Pipeline.BackupDir,
		WriteBack: process.WriteBackOptions{
			Enabled:             writeBack,
			DestinationFolderID: writeBackFolder,
			Verify:              verifyWriteBack,
		},
	}
}

func buildDiscoverService(cfg appconfig.Config) (*discover.Service, error) {
	_, reader, writer, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	return &discover.Service{
		Client:    reader,
		Processor: buildProcessServiceWithProvider(cfg, reader, writer),
	}, nil
}

func syncConfig(defaults appconfig.Config, clientID, tenant, stateDir, authorityBaseURL, graphBaseURL string) appconfig.Config {
	cfg := defaults
	previousStateDir := cfg.Mail.Sync.StateDir
	previousAuditLogPath := cfg.Mail.Pipeline.AuditLogPath
	cfg.Auth.ClientID = clientID
	cfg.Auth.Tenant = tenant
	cfg.Auth.StateDir = stateDir
	cfg.Auth.AuthorityBaseURL = authorityBaseURL
	cfg.Mail.Client.GraphBaseURL = graphBaseURL
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

func buildCatchAllBackupEncryptor(cfg appconfig.Config) *encrypt.Service {
	recipients := normalizeRecipientSpecs([]string{cfg.Mail.Pipeline.BackupKeyID})
	if len(recipients) == 0 {
		return nil
	}
	return buildLocalEncryptService(recipients, "")
}
