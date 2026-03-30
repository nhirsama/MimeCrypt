package flowruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/mailflow/adapters"
	"mimecrypt/internal/providers"
)

const defaultPushIdlePollInterval = time.Second

type PushRuntime struct {
	Runner           *mailflow.Runner
	Ingress          providers.PushIngress
	IdlePollInterval time.Duration
}

func BuildPushRuntime(ctx context.Context, run SourceRun) (*PushRuntime, error) {
	if !strings.EqualFold(strings.TrimSpace(run.Source.Mode), "push") {
		return nil, fmt.Errorf("push runtime 仅支持 mode=push，当前 source=%s mode=%s", run.Source.Name, run.Source.Mode)
	}

	coordinator, err := buildCoordinatorForMode(ctx, run, TransactionModePersistent)
	if err != nil {
		return nil, err
	}

	spool := &adapters.PushSpool{
		Dir:             pushSpoolDirForSource(run.Route.StateDir, run.Source),
		ReplayRetention: replayRetentionForSource(run.Source),
	}
	ingress, err := providers.BuildPushIngress(run.Config, run.Route, run.Source, spool)
	if err != nil {
		return nil, err
	}

	sourceStore := mailflow.StoreRef{
		Driver:  normalizeDriver(run.Source.Driver, ""),
		Account: firstNonEmpty(strings.TrimSpace(run.Source.Name), normalizeDriver(run.Source.Driver, "source")),
	}

	return &PushRuntime{
		Runner: &mailflow.Runner{
			Producer: &adapters.PushProducer{
				Name:            run.Source.Name,
				Driver:          run.Source.Driver,
				Store:           sourceStore,
				Spool:           spool,
				DeleteSemantics: run.SourceDeleteSemantics,
			},
			Coordinator: coordinator,
		},
		Ingress:          ingress,
		IdlePollInterval: defaultPushIdlePollInterval,
	}, nil
}

func (r *PushRuntime) Run(ctx context.Context) error {
	if r == nil || r.Runner == nil || r.Ingress == nil {
		return fmt.Errorf("push runtime 未初始化")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)
	go func() {
		errCh <- r.Ingress.Run(runCtx)
	}()
	go func() {
		errCh <- r.runWorker(runCtx)
	}()

	for remaining := 2; remaining > 0; {
		select {
		case <-ctx.Done():
			cancel()
			return ctx.Err()
		case err := <-errCh:
			remaining--
			if err == nil || errors.Is(err, context.Canceled) {
				continue
			}
			cancel()
			return err
		}
	}
	return nil
}

func (r *PushRuntime) runWorker(ctx context.Context) error {
	idlePollInterval := r.IdlePollInterval
	if idlePollInterval <= 0 {
		idlePollInterval = defaultPushIdlePollInterval
	}

	for {
		_, processed, err := r.Runner.RunOnce(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			return err
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(idlePollInterval):
		}
	}
}

func replayRetentionForSource(source appconfig.Source) time.Duration {
	if source.Webhook != nil && source.Webhook.TimestampTolerance > 0 {
		return source.Webhook.TimestampTolerance
	}
	return 5 * time.Minute
}
