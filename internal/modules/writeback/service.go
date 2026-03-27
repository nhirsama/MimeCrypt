package writeback

import (
	"context"
	"errors"

	"mimecrypt/internal/provider"
)

var ErrNotImplemented = errors.New("回写邮件并校验功能尚未实现")

type Service struct {
	Writer provider.Writer
}

type Request struct {
	MessageID string
	MIME      []byte
	Verify    bool
}

type Result struct {
	Verified bool
}

// Run 预留给 IMAP APPEND 和校验逻辑，当前只保留模块边界。
func (s *Service) Run(ctx context.Context, req Request) (Result, error) {
	if s.Writer == nil {
		return Result{}, ErrNotImplemented
	}

	result, err := s.Writer.WriteMessage(ctx, provider.WriteRequest{
		MessageID: req.MessageID,
		MIME:      req.MIME,
		Verify:    req.Verify,
	})
	if err != nil {
		if errors.Is(err, provider.ErrNotSupported) {
			return Result{}, ErrNotImplemented
		}
		return Result{}, err
	}

	return Result{Verified: result.Verified}, nil
}
