package download

import (
	"context"
	"fmt"
	"io"

	"mimecrypt/internal/mimefile"
	"mimecrypt/internal/provider"
)

type Service struct {
	Client provider.Reader
}

type Payload struct {
	Message provider.Message
	MIME    []byte
}

type Result struct {
	Message provider.Message
	Path    string
	Bytes   int64
}

// Fetch 按邮件 ID 获取元数据和 MIME 字节。
func (s *Service) Fetch(ctx context.Context, messageID string) (Payload, error) {
	message, err := s.Client.Message(ctx, messageID)
	if err != nil {
		return Payload{}, fmt.Errorf("获取邮件元数据失败: %w", err)
	}

	stream, err := s.Client.FetchMIME(ctx, messageID)
	if err != nil {
		return Payload{}, fmt.Errorf("获取邮件 MIME 失败: %w", err)
	}
	defer stream.Close()

	mimeBytes, err := io.ReadAll(stream)
	if err != nil {
		return Payload{}, fmt.Errorf("读取邮件 MIME 失败: %w", err)
	}

	return Payload{
		Message: message,
		MIME:    mimeBytes,
	}, nil
}

// Save 按邮件 ID 下载并保存 MIME 文件。
func (s *Service) Save(ctx context.Context, messageID, outputDir string) (Result, error) {
	payload, err := s.Fetch(ctx, messageID)
	if err != nil {
		return Result{}, err
	}

	return s.SavePayload(payload, outputDir)
}

// SavePayload 将已经获取到的 MIME 负载保存到输出目录。
func (s *Service) SavePayload(payload Payload, outputDir string) (Result, error) {
	path, written, err := mimefile.SaveBytesToOutputDir(outputDir, payload.Message, payload.MIME)
	if err != nil {
		return Result{}, fmt.Errorf("保存邮件 MIME 失败: %w", err)
	}

	return Result{
		Message: payload.Message,
		Path:    path,
		Bytes:   written,
	}, nil
}
