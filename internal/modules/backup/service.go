package backup

import (
	"fmt"
	"io"

	"mimecrypt/internal/provider"
)

type Service struct{}

type Request struct {
	Message          provider.Message
	Ciphertext       []byte
	CiphertextOpener func() (io.ReadCloser, error)
	Dir              string
}

type Result struct {
	Path  string
	Bytes int64
}

func (s *Service) Run(req Request) (Result, error) {
	var (
		path    string
		written int64
		err     error
	)
	if req.CiphertextOpener != nil {
		reader, openErr := req.CiphertextOpener()
		if openErr != nil {
			return Result{}, fmt.Errorf("打开加密备份源失败: %w", openErr)
		}
		defer reader.Close()
		path, written, err = saveToDir(req.Dir, req.Message, ".pgp", reader)
	} else {
		path, written, err = SaveCiphertext(req.Dir, req.Message, req.Ciphertext)
	}
	if err != nil {
		return Result{}, fmt.Errorf("保存加密备份失败: %w", err)
	}

	return Result{
		Path:  path,
		Bytes: written,
	}, nil
}
