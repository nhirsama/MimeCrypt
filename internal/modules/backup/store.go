package backup

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"mimecrypt/internal/mimefile"
	"mimecrypt/internal/provider"
)

func SaveCiphertext(dir string, message provider.Message, ciphertext []byte) (string, int64, error) {
	if len(ciphertext) == 0 {
		return "", 0, fmt.Errorf("加密备份内容不能为空")
	}

	return saveToDir(dir, message, ".pgp", bytes.NewReader(ciphertext))
}

func saveToDir(dir string, message provider.Message, ext string, src io.Reader) (string, int64, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", 0, fmt.Errorf("创建备份目录失败: %w", err)
	}

	fileName := buildBackupFileName(message, ext)
	path := filepath.Join(dir, fileName)

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("创建备份文件失败: %w", err)
	}

	written, copyErr := io.Copy(file, src)
	closeErr := file.Close()
	if copyErr != nil {
		return "", written, fmt.Errorf("写入备份文件失败: %w", copyErr)
	}
	if closeErr != nil {
		return "", written, fmt.Errorf("关闭备份文件失败: %w", closeErr)
	}

	return path, written, nil
}

func buildBackupFileName(message provider.Message, ext string) string {
	base := mimefile.BuildMessageFileStem(message)
	return base + ext
}
