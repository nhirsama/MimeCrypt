//go:build unix

package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type tokenStoreFileLock struct {
	file *os.File
}

func acquireTokenStoreFileLock(path string) (*tokenStoreFileLock, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("token store lock path 不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("创建 token store 锁目录失败: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开 token store 锁文件失败: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("获取 token store 锁失败: %w", err)
	}

	return &tokenStoreFileLock{file: file}, nil
}

func (l *tokenStoreFileLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}

	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		_ = l.file.Close()
		return fmt.Errorf("释放 token store 锁失败: %w", err)
	}
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("关闭 token store 锁文件失败: %w", err)
	}
	l.file = nil
	return nil
}
