package fileutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteFileAtomic 将内容先写入目标目录下的临时文件，再通过 rename 原子替换目标文件。
func WriteFileAtomic(path string, perm os.FileMode, src io.Reader) (int64, error) {
	if src == nil {
		return 0, fmt.Errorf("写入源不能为空")
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)

	file, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return 0, fmt.Errorf("创建临时文件失败: %w", err)
	}

	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}

	if err := file.Chmod(perm); err != nil {
		cleanup()
		return 0, fmt.Errorf("设置临时文件权限失败: %w", err)
	}

	written, err := io.Copy(file, src)
	if err != nil {
		cleanup()
		return written, fmt.Errorf("写入临时文件失败: %w", err)
	}

	if err := file.Sync(); err != nil {
		cleanup()
		return written, fmt.Errorf("同步临时文件失败: %w", err)
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return written, fmt.Errorf("关闭临时文件失败: %w", err)
	}

	if err := os.Rename(file.Name(), path); err != nil {
		_ = os.Remove(file.Name())
		return written, fmt.Errorf("替换目标文件失败: %w", err)
	}

	if err := syncDir(dir); err != nil {
		return written, fmt.Errorf("同步目标目录失败: %w", err)
	}

	return written, nil
}

func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer handle.Close()

	return handle.Sync()
}
