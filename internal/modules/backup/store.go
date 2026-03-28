package backup

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"mimecrypt/internal/fileutil"
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

	written, err := fileutil.WriteFileAtomic(path, 0o600, src)
	if err != nil {
		return "", written, fmt.Errorf("写入备份文件失败: %w", err)
	}

	return path, written, nil
}

func buildBackupFileName(message provider.Message, ext string) string {
	base := mimefile.BuildMessageFileStem(message)
	return base + ext
}
