package flowruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/mailflow/adapters"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers"
	webhookdevice "mimecrypt/internal/providers/webhook"
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
	executor := &SourceExecutor{
		Runner:           r.Runner,
		Ingress:          r.Ingress,
		IdlePollInterval: r.IdlePollInterval,
	}
	return executor.Run(ctx)
}

func replayRetentionForSource(source appconfig.Source) time.Duration {
	if strings.EqualFold(strings.TrimSpace(source.Driver), "webhook") && len(source.DriverConfig) > 0 {
		config, err := webhookdevice.DecodeSourceConfig(source)
		if err == nil && config.TimestampTolerance > 0 {
			return config.TimestampTolerance
		}
	}
	return 5 * time.Minute
}
