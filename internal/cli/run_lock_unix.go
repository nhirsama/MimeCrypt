//go:build unix

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type runLock struct {
	file *os.File
	path string
}

func acquireRunLock(path string) (*runLock, error) {
	if path == "" {
		return nil, fmt.Errorf("run lock path 不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("创建运行锁目录失败: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开运行锁文件失败: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("%w: %s", ErrRunLocked, path)
		}
		return nil, fmt.Errorf("获取运行锁失败: %w", err)
	}
	if err := file.Truncate(0); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, fmt.Errorf("清空运行锁文件失败: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, fmt.Errorf("重置运行锁文件偏移失败: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, fmt.Errorf("写入运行锁文件失败: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, fmt.Errorf("刷新运行锁文件失败: %w", err)
	}

	return &runLock{file: file, path: path}, nil
}

func (l *runLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}

	var errs []error
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		errs = append(errs, fmt.Errorf("释放运行锁失败: %w", err))
	}
	if err := l.file.Close(); err != nil {
		errs = append(errs, fmt.Errorf("关闭运行锁文件失败: %w", err))
	}
	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("删除运行锁文件失败: %w", err))
	}
	l.file = nil

	return errors.Join(errs...)
}
