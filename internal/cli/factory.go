package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
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

	return buildProcessServiceWithProvider(reader, writer), nil
}

func buildDownloadServiceWithReader(reader provider.Reader) *download.Service {
	return &download.Service{Client: reader}
}

func buildProcessServiceWithProvider(reader provider.Reader, writer provider.Writer) *process.Service {
	return &process.Service{
		Downloader: buildDownloadServiceWithReader(reader),
		Encryptor:  &encrypt.Service{},
		WriteBack:  &writeback.Service{Writer: writer},
	}
}

func buildDiscoverService(cfg appconfig.Config) (*discover.Service, error) {
	_, reader, writer, err := buildProviderClients(cfg)
	if err != nil {
		return nil, err
	}

	return &discover.Service{
		Client:    reader,
		Processor: buildProcessServiceWithProvider(reader, writer),
	}, nil
}

func syncConfig(defaults appconfig.Config, clientID, tenant, stateDir, authorityBaseURL, graphBaseURL string) appconfig.Config {
	cfg := defaults
	cfg.Auth.ClientID = clientID
	cfg.Auth.Tenant = tenant
	cfg.Auth.StateDir = stateDir
	cfg.Auth.AuthorityBaseURL = authorityBaseURL
	cfg.Mail.GraphBaseURL = graphBaseURL
	cfg.Mail.StateDir = stateDir
	return cfg
}

func validateWriteBackFlags(writeBack, verifyWriteBack bool) error {
	if verifyWriteBack && !writeBack {
		return fmt.Errorf("--verify-write-back 依赖 --write-back")
	}

	return nil
}
