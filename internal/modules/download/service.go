package download

import (
	"context"
	"fmt"
	"io"
	"os"

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

type TempPayload struct {
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

// FetchToTemp 按邮件 ID 获取元数据并把 MIME 流写入临时文件。
func (s *Service) FetchToTemp(ctx context.Context, messageID, tempDir string) (TempPayload, error) {
	message, err := s.Client.Message(ctx, messageID)
	if err != nil {
		return TempPayload{}, fmt.Errorf("获取邮件元数据失败: %w", err)
	}

	stream, err := s.Client.FetchMIME(ctx, messageID)
	if err != nil {
		return TempPayload{}, fmt.Errorf("获取邮件 MIME 失败: %w", err)
	}
	defer stream.Close()

	file, err := createTempMIMEFile(tempDir)
	if err != nil {
		return TempPayload{}, err
	}

	written, copyErr := io.Copy(file, stream)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(file.Name())
		return TempPayload{}, fmt.Errorf("写入临时 MIME 文件失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(file.Name())
		return TempPayload{}, fmt.Errorf("关闭临时 MIME 文件失败: %w", closeErr)
	}

	return TempPayload{
		Message: message,
		Path:    file.Name(),
		Bytes:   written,
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

// SaveStream 将 MIME 流保存到输出目录。
func (s *Service) SaveStream(message provider.Message, src io.Reader, outputDir string) (Result, error) {
	path, written, err := mimefile.SaveToOutputDir(outputDir, message, src)
	if err != nil {
		return Result{}, fmt.Errorf("保存邮件 MIME 失败: %w", err)
	}

	return Result{
		Message: message,
		Path:    path,
		Bytes:   written,
	}, nil
}

func createTempMIMEFile(tempDir string) (*os.File, error) {
	if tempDir != "" {
		if err := os.MkdirAll(tempDir, 0o700); err != nil {
			return nil, fmt.Errorf("创建临时目录失败: %w", err)
		}
	}

	file, err := os.CreateTemp(tempDir, "mimecrypt-*.eml")
	if err != nil {
		return nil, fmt.Errorf("创建临时 MIME 文件失败: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, fmt.Errorf("设置临时 MIME 文件权限失败: %w", err)
	}
	return file, nil
}
