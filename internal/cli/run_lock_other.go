//go:build !unix

package cli

import "fmt"

type runLock struct{}

func acquireRunLock(string) (*runLock, error) {
	return nil, fmt.Errorf("当前平台未实现 run 锁")
}

func (l *runLock) Release() error {
	return nil
}
