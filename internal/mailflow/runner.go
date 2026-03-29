package mailflow

import (
	"context"
	"errors"
	"fmt"
)

// Runner 将 Producer 与 Coordinator 串联成可复用的单次执行入口。
type Runner struct {
	Producer    Producer
	Coordinator *Coordinator
}

// RunOnce 拉取一封邮件并执行一次完整事务。
// 当当前没有待处理邮件时，processed 返回 false 且 err 为 nil。
func (r *Runner) RunOnce(ctx context.Context) (Result, bool, error) {
	if r == nil || r.Producer == nil {
		return Result{}, false, fmt.Errorf("producer 未配置")
	}
	if r.Coordinator == nil {
		return Result{}, false, fmt.Errorf("coordinator 未配置")
	}

	envelope, err := r.Producer.Next(ctx)
	if err != nil {
		if errors.Is(err, ErrNoMessages) {
			return Result{}, false, nil
		}
		return Result{}, false, err
	}

	result, err := r.Coordinator.Run(ctx, envelope)
	if err != nil {
		return Result{}, true, err
	}
	return result, true, nil
}
