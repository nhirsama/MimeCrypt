package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/modules/download"
	"mimecrypt/internal/modules/health"
	"mimecrypt/internal/modules/list"
	"mimecrypt/internal/modules/login"
	"mimecrypt/internal/modules/logout"
	"mimecrypt/internal/modules/tokenstate"
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

func buildLoginService(cfg appconfig.Config) (*login.Service, error) {
	sourceClients, err := providers.BuildSourceClients(cfg)
	if err != nil {
		return nil, err
	}

	return &login.Service{
		Session:  sourceClients.Session,
		Mail:     sourceClients.Reader,
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
	sourceClients, err := providers.BuildSourceClients(cfg)
	if err != nil {
		return nil, err
	}
	return buildDownloadServiceWithReader(sourceClients.Reader), nil
}

func buildListService(cfg appconfig.Config) (*list.Service, error) {
	sourceClients, err := providers.BuildSourceClients(cfg)
	if err != nil {
		return nil, err
	}
	return buildListServiceWithReader(sourceClients.Reader), nil
}

func buildHealthService(cfg appconfig.Config) (*health.Service, error) {
	sourceClients, err := providers.BuildSourceClients(cfg)
	if err != nil {
		return nil, err
	}
	sinkClients, err := providers.BuildWriteBackClientsWithSession(cfg, sourceClients.Session)
	if err != nil {
		return nil, err
	}

	return &health.Service{
		StateDir:          cfg.Auth.StateDir,
		Folder:            cfg.Mail.Sync.Folder,
		Provider:          cfg.Provider,
		WriteBackProvider: cfg.Mail.Pipeline.WriteBackProvider,
		Session:           sourceClients.Session,
		Reader:            sourceClients.Reader,
		WriteBack:         sinkClients.Health,
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

func buildDownloadServiceWithReader(reader provider.Reader) *download.Service {
	return &download.Service{Client: reader}
}

func buildListServiceWithReader(reader provider.Reader) *list.Service {
	return &list.Service{Client: reader}
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
