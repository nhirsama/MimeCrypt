package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/flowruntime"
	"mimecrypt/internal/mailflow"
)

func newRunCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("run", "执行邮件发现、处理与回写流程", err)
	}

	topologyFlags := newTopologyConfigFlags(cfg)
	pipelineFlags := newPipelineConfigFlags(cfg)
	var once bool
	var debugSaveFirst bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "执行邮件发现、处理与回写流程",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg = topologyFlags.apply(cfg)
			cfg = pipelineFlags.apply(cfg, cmd)

			if err := cfg.Mail.ValidatePipelineBase(); err != nil {
				return fmt.Errorf("run 失败: %w", err)
			}

			resolved, err := flowruntime.ResolveRoutePlan(cfg, flowruntime.Selector{
				RouteName:  strings.TrimSpace(topologyFlags.routeName),
				SourceName: strings.TrimSpace(topologyFlags.sourceName),
			}, flowruntime.RoutePlanAllSources)
			if err != nil {
				return fmt.Errorf("run 失败: %w", err)
			}
			locks, err := acquireRouteLocks(resolved.Runs)
			if err != nil {
				return fmt.Errorf("run 失败: %w", err)
			}
			defer func() {
				releaseRouteLocks(locks)
			}()

			if debugSaveFirst {
				if len(resolved.Runs) != 1 {
					return fmt.Errorf("run 失败: --debug-save-first 需要显式选择单个 source")
				}
				return runDebugSaveFirst(cmd.Context(), resolved.Runs[0])
			}

			configuredRuns, err := buildConfiguredRuns(cmd.Context(), resolved.Runs)
			if err != nil {
				return fmt.Errorf("run 失败: %w", err)
			}

			if once {
				if err := runAllSourcesOnce(cmd.Context(), configuredRuns); err != nil {
					return fmt.Errorf("run 失败: %w", err)
				}
				return nil
			}

			if err := runStartupSources(cmd.Context(), configuredRuns); err != nil {
				return fmt.Errorf("run 失败: %w", err)
			}

			return runRouteLoops(cmd.Context(), configuredRuns)
		},
	}

	topologyFlags.addFlags(cmd)
	pipelineFlags.addFlags(cmd)
	cmd.Flags().BoolVar(&once, "once", false, "执行一个同步周期后退出")
	cmd.Flags().BoolVar(&debugSaveFirst, "debug-save-first", false, "调试模式下处理当前文件夹中最新的一封邮件并退出")

	return cmd
}

type configuredSourceRun struct {
	Run    flowruntime.SourceRun
	Runner interface {
		RunOnce(context.Context) (mailflow.Result, bool, error)
	}
	Push *flowruntime.PushRuntime
}

func runMailflowCycle(ctx context.Context, cycleTimeout time.Duration, runner interface {
	RunOnce(context.Context) (mailflow.Result, bool, error)
}) (int, int, int, error) {
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

func buildConfiguredRuns(ctx context.Context, runs []flowruntime.SourceRun) ([]configuredSourceRun, error) {
	configured := make([]configuredSourceRun, 0, len(runs))
	for _, run := range runs {
		if strings.EqualFold(strings.TrimSpace(run.Source.Mode), "push") {
			pushRuntime, err := flowruntime.BuildPushRuntime(ctx, run)
			if err != nil {
				return nil, err
			}
			configured = append(configured, configuredSourceRun{
				Run:    run,
				Runner: pushRuntime.Runner,
				Push:   pushRuntime,
			})
			continue
		}

		runner, err := flowruntime.BuildRunner(ctx, run)
		if err != nil {
			return nil, err
		}
		configured = append(configured, configuredSourceRun{
			Run:    run,
			Runner: runner,
		})
	}
	return configured, nil
}

func runAllSourcesOnce(ctx context.Context, runs []configuredSourceRun) error {
	includeSourcePrefix := len(runs) > 1
	for _, run := range runs {
		if err := runConfiguredSourceOnce(ctx, run, includeSourcePrefix); err != nil {
			return err
		}
	}
	return nil
}

func runStartupSources(ctx context.Context, runs []configuredSourceRun) error {
	includeSourcePrefix := len(runs) > 1
	for _, run := range runs {
		if run.Push != nil {
			continue
		}
		if err := runConfiguredSourceOnce(ctx, run, includeSourcePrefix); err != nil {
			return err
		}
	}
	return nil
}

func runRouteLoops(ctx context.Context, runs []configuredSourceRun) error {
	includeSourcePrefix := len(runs) > 1
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	for _, run := range runs {
		run := run
		wg.Add(1)
		go func() {
			defer wg.Done()

			if run.Push != nil {
				if err := run.Push.Run(loopCtx); err != nil && !errors.Is(err, context.Canceled) {
					select {
					case errCh <- fmt.Errorf("source=%s: %w", run.Run.Source.Name, err):
					default:
					}
					cancel()
				}
				return
			}

			ticker := time.NewTicker(run.Run.Source.PollInterval)
			defer ticker.Stop()

			for {
				select {
				case <-loopCtx.Done():
					return
				case <-ticker.C:
					if err := runConfiguredSourceOnce(loopCtx, run, includeSourcePrefix); err != nil {
						fmt.Printf("source=%s 本轮同步失败，下个周期继续重试: %v\n", run.Run.Source.Name, err)
					}
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		cancel()
		<-done
		return ctx.Err()
	case err := <-errCh:
		cancel()
		<-done
		return err
	case <-done:
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}
}

func runConfiguredSourceOnce(ctx context.Context, run configuredSourceRun, includeSourcePrefix bool) error {
	processedCount, skippedCount, deletedCount, err := runMailflowCycle(ctx, run.Run.Source.CycleTimeout, run.Runner)
	if err != nil {
		return fmt.Errorf("source=%s: %w", run.Run.Source.Name, err)
	}
	if processedCount == 0 && skippedCount == 0 {
		if !includeSourcePrefix || strings.TrimSpace(run.Run.Source.Name) == "" {
			fmt.Printf("本轮无待处理邮件\n")
		} else {
			fmt.Printf("source=%s 本轮无待处理邮件\n", run.Run.Source.Name)
		}
		return nil
	}
	if !includeSourcePrefix || strings.TrimSpace(run.Run.Source.Name) == "" {
		fmt.Printf("同步完成，本轮处理 %d 封邮件，跳过 %d 封，删除源邮件 %d 封\n", processedCount, skippedCount, deletedCount)
		return nil
	}
	fmt.Printf("source=%s 同步完成，本轮处理 %d 封邮件，跳过 %d 封，删除源邮件 %d 封\n", run.Run.Source.Name, processedCount, skippedCount, deletedCount)
	return nil
}

func acquireRouteLocks(runs []flowruntime.SourceRun) ([]*runLock, error) {
	paths := make([]string, 0, len(runs))
	for _, run := range runs {
		if path := strings.TrimSpace(run.LockPath); path != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	locks := make([]*runLock, 0, len(paths))
	for _, path := range paths {
		lock, err := acquireRunLock(path)
		if err != nil {
			releaseRouteLocks(locks)
			return nil, err
		}
		locks = append(locks, lock)
	}
	return locks, nil
}

func releaseRouteLocks(locks []*runLock) {
	for i := len(locks) - 1; i >= 0; i-- {
		_ = locks[i].Release()
	}
}

func runDebugSaveFirst(ctx context.Context, run flowruntime.SourceRun) error {
	if !strings.EqualFold(strings.TrimSpace(run.Source.Mode), "poll") {
		return fmt.Errorf("--debug-save-first 仅支持 mode=poll 的 source，当前 source=%s mode=%s", run.Source.Name, run.Source.Mode)
	}

	cycleCtx, cancel := context.WithTimeout(ctx, run.Source.CycleTimeout)
	defer cancel()

	runner, err := flowruntime.BuildSingleMessageRunner(cycleCtx, run, flowruntime.TransactionModeEphemeral)
	if err != nil {
		return err
	}
	result, found, err := runner.RunFirstMessage(cycleCtx)
	if err != nil {
		return err
	}
	if !found {
		fmt.Printf("调试模式未找到可处理的邮件，source=%s folder=%s\n", run.Source.Name, run.Source.Folder)
		return nil
	}
	summary, err := summarizeSingleMessageResult(result)
	if err != nil {
		return err
	}

	fmt.Printf(
		"调试模式已处理第一封邮件，message_id=%s format=%s encrypted=%t saved_output=%t backup_path=%s path=%s bytes=%d\n",
		summary.MessageID,
		summary.Format,
		summary.Encrypted,
		summary.SavedOutput,
		summary.BackupPath,
		summary.Path,
		summary.Bytes,
	)
	return nil
}
