package mimeutil

import (
	"bytes"
	"fmt"
	"io"
	"net/mail"
	"strings"
)

const processedHeader = "X-MimeCrypt-Processed"

// IsProcessedEncryptedBytes 检查整封 MIME 是否带有 MimeCrypt 处理标记和 PGP/MIME 顶层类型。
func IsProcessedEncryptedBytes(mimeBytes []byte) bool {
	ok, err := IsProcessedEncryptedStream(bytes.NewReader(mimeBytes))
	return err == nil && ok
}

// IsProcessedEncryptedStream 只解析 MIME 头部，避免为校验把整封邮件读入内存。
func IsProcessedEncryptedStream(src io.Reader) (bool, error) {
	if src == nil {
		return false, fmt.Errorf("MIME 输入源不能为空")
	}

	message, err := mail.ReadMessage(src)
	if err != nil {
		return false, fmt.Errorf("解析 MIME 头失败: %w", err)
	}

	return HasProcessedEncryptedHeader(message.Header), nil
}

// HasProcessedEncryptedHeader 判断邮件头是否表示 MimeCrypt 处理过的 PGP/MIME 邮件。
func HasProcessedEncryptedHeader(header mail.Header) bool {
	if !strings.EqualFold(strings.TrimSpace(header.Get(processedHeader)), "yes") {
		return false
	}

	contentType := strings.ToLower(strings.TrimSpace(header.Get("Content-Type")))
	return strings.Contains(contentType, "multipart/encrypted") &&
		strings.Contains(contentType, "application/pgp-encrypted")
}
