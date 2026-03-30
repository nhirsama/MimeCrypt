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
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers"
)

const defaultPushIdlePollInterval = time.Second

type PushRuntime struct {
	Runner           *mailflow.Runner
	Ingress          provider.SourceIngress
	IdlePollInterval time.Duration
}

func BuildPushRuntime(ctx context.Context, run SourceRun) (*PushRuntime, error) {
	mode := strings.TrimSpace(run.Source.Mode)
	if !strings.EqualFold(mode, "push") {
		return nil, fmt.Errorf("push runtime 仅支持 mode=push，当前 source=%s mode=%s", run.Source.Name, run.Source.Mode)
	}
	sourceSpec, ok := providers.LookupSourceSpec(run.Source.Driver)
	if !ok {
		return nil, fmt.Errorf("run 不支持 source driver=%s", run.Source.Driver)
	}
	if _, ok := sourceSpec.ModeSpec(mode); !ok {
		return nil, fmt.Errorf("run 不支持 source=%s driver=%s mode=%s", run.Source.Name, run.Source.Driver, mode)
	}

	source, err := buildSourceRuntimeBundle(run)
	if err != nil {
		return nil, err
	}
	if source.Ingress == nil {
		return nil, fmt.Errorf("source driver %s 未提供 mode=push runtime", run.Source.Driver)
	}
	if source.Spool == nil {
		return nil, fmt.Errorf("source %s 未初始化 push spool", run.Source.Name)
	}

	coordinator, err := buildCoordinatorForMode(ctx, run, TransactionModePersistent)
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
				Spool:           source.Spool,
				DeleteSemantics: run.SourceDeleteSemantics,
			},
			Coordinator: coordinator,
		},
		Ingress:          source.Ingress,
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
