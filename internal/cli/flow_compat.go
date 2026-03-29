package cli

import (
	"context"
	"fmt"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/provider"
)

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

func applyTopologyCredential(cfg appconfig.Config, topology appconfig.Topology, credentialRef string) (appconfig.Config, error) {
	credentialRef = strings.TrimSpace(credentialRef)
	if credentialRef == "" {
		return cfg, nil
	}
	credential, ok := topology.Credentials[credentialRef]
	if !ok {
		return appconfig.Config{}, fmt.Errorf("credential 不存在: %s", credentialRef)
	}
	return cfg.WithCredential(credential.Name, credential), nil
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
