package process

import (
	"context"

	"mimecrypt/internal/modules/download"
	"mimecrypt/internal/modules/encrypt"
	"mimecrypt/internal/modules/writeback"
)

type Downloader interface {
	Fetch(ctx context.Context, messageID string) (download.Payload, error)
	SavePayload(payload download.Payload, outputDir string) (download.Result, error)
}

type Encryptor interface {
	Run(mimeBytes []byte) (encrypt.Result, error)
}

type Writer interface {
	Run(ctx context.Context, req writeback.Request) (writeback.Result, error)
}

type Service struct {
	Downloader Downloader
	Encryptor  Encryptor
	WriteBack  Writer
}

type Request struct {
	MessageID       string
	OutputDir       string
	WriteBack       bool
	VerifyWriteBack bool
}

type Result struct {
	MessageID        string
	Path             string
	Bytes            int64
	Encrypted        bool
	AlreadyEncrypted bool
	Format           string
	WroteBack        bool
	Verified         bool
}

// Run 根据邮件 ID 和配置处理邮件。
func (s *Service) Run(ctx context.Context, req Request) (Result, error) {
	payload, err := s.Downloader.Fetch(ctx, req.MessageID)
	if err != nil {
		return Result{}, err
	}

	encryptResult, err := s.Encryptor.Run(payload.MIME)
	if err != nil {
		return Result{}, err
	}

	payload.MIME = encryptResult.MIME
	saveResult, err := s.Downloader.SavePayload(payload, req.OutputDir)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		MessageID:        req.MessageID,
		Path:             saveResult.Path,
		Bytes:            saveResult.Bytes,
		Encrypted:        encryptResult.Encrypted,
		AlreadyEncrypted: encryptResult.AlreadyEncrypted,
		Format:           encryptResult.Format,
	}

	if req.WriteBack && s.WriteBack != nil {
		writeBackResult, err := s.WriteBack.Run(ctx, writeback.Request{
			MessageID: req.MessageID,
			MIME:      encryptResult.MIME,
			Verify:    req.VerifyWriteBack,
		})
		if err != nil {
			return Result{}, err
		}

		result.WroteBack = true
		result.Verified = writeBackResult.Verified
	}

	return result, nil
}
