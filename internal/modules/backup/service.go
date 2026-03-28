package backup

import (
	"fmt"

	"mimecrypt/internal/provider"
)

type Service struct{}

type Request struct {
	Message    provider.Message
	Ciphertext []byte
	Dir        string
}

type Result struct {
	Path  string
	Bytes int64
}

func (s *Service) Run(req Request) (Result, error) {
	path, written, err := SaveCiphertext(req.Dir, req.Message, req.Ciphertext)
	if err != nil {
		return Result{}, fmt.Errorf("保存加密备份失败: %w", err)
	}

	return Result{
		Path:  path,
		Bytes: written,
	}, nil
}
