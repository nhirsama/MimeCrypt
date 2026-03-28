package mimefile

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"mimecrypt/internal/fileutil"
	"mimecrypt/internal/provider"
)

// SaveToOutputDir 将抓取到的 MIME 流保存到输出目录。
func SaveToOutputDir(outputDir string, message provider.Message, src io.Reader) (string, int64, error) {
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", 0, fmt.Errorf("创建输出目录失败: %w", err)
	}

	fileName := buildMessageFileName(message)
	path := filepath.Join(outputDir, fileName)

	written, err := fileutil.WriteFileAtomic(path, 0o600, src)
	if err != nil {
		return "", written, fmt.Errorf("写入 MIME 流到文件失败: %w", err)
	}

	return path, written, nil
}

// SaveBytesToOutputDir 将 MIME 字节保存到输出目录。
func SaveBytesToOutputDir(outputDir string, message provider.Message, mimeBytes []byte) (string, int64, error) {
	return SaveToOutputDir(outputDir, message, bytes.NewReader(mimeBytes))
}

func buildMessageFileName(message provider.Message) string {
	return BuildMessageFileStem(message) + ".eml"
}

func BuildMessageFileStem(message provider.Message) string {
	prefix := "message"
	if !message.ReceivedDateTime.IsZero() {
		prefix = message.ReceivedDateTime.UTC().Format("20060102T150405Z")
	}

	return fmt.Sprintf("%s_%s", prefix, sanitizeFileComponent(message.ID))
}

func sanitizeFileComponent(value string) string {
	if value == "" {
		return "unknown"
	}

	var builder strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(r)
		case r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}

	result := strings.Trim(builder.String(), "._")
	if result == "" {
		return "unknown"
	}

	return result
}
