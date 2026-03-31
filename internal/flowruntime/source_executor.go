package flowruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/provider"
)

type SourceExecutor struct {
	Spec             SourceRun
	Runner           *mailflow.Runner
	Ingress          provider.SourceIngress
	IdlePollInterval time.Duration
}

func (e *SourceExecutor) IsPush() bool {
	return e != nil && e.Ingress != nil
}

func (e *SourceExecutor) RunCycle(ctx context.Context) (int, int, int, error) {
	if e == nil || e.Runner == nil {
		return 0, 0, 0, fmt.Errorf("source executor 未初始化")
	}
	return runSourceCycle(ctx, e.Spec.Source.CycleTimeout, e.Runner)
}

func BuildRunner(ctx context.Context, run SourceRun) (*mailflow.Runner, error) {
	if strings.EqualFold(run.Source.Mode, "push") {
		return nil, fmt.Errorf("run 尚未实现 source=%s 的 mode=%s", run.Source.Name, run.Source.Mode)
	}
	executor, err := BuildSourceExecutor(ctx, run)
	if err != nil {
		return nil, err
	}
	if executor.IsPush() {
		return nil, fmt.Errorf("run 尚未实现 source=%s 的 mode=%s", run.Source.Name, run.Source.Mode)
	}
	return executor.Runner, nil
}

func runSourceCycle(ctx context.Context, cycleTimeout time.Duration, runner *mailflow.Runner) (int, int, int, error) {
	cycleCtx := ctx
	cancel := func() {}
	if cycleTimeout > 0 {
		cycleCtx, cancel = context.WithTimeout(ctx, cycleTimeout)
	} else {
		cycleCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	processedCount := 0
	skippedCount := 0
	deletedCount := 0
	for {
		result, processed, err := runner.RunOnce(cycleCtx)
		if err != nil {
			return processedCount, skippedCount, deletedCount, err
		}
		if !processed {
			return processedCount, skippedCount, deletedCount, nil
		}
		if result.Skipped {
			skippedCount++
		} else {
			processedCount++
		}
		if result.SourceDeleted {
			deletedCount++
		}
	}
}

func (e *SourceExecutor) Run(ctx context.Context) error {
	if e == nil || e.Runner == nil || e.Ingress == nil {
		return fmt.Errorf("push source executor 未初始化")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)
	go func() {
		errCh <- e.Ingress.Run(runCtx)
	}()
	go func() {
		errCh <- e.runPushWorker(runCtx)
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

func (e *SourceExecutor) runPushWorker(ctx context.Context) error {
	idlePollInterval := e.IdlePollInterval
	if idlePollInterval <= 0 {
		idlePollInterval = defaultPushIdlePollInterval
	}

	for {
		_, processed, err := e.Runner.RunOnce(ctx)
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
