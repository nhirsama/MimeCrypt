package writeback

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"mimecrypt/internal/provider"
)

var ErrNotImplemented = errors.New("回写邮件并校验功能尚未实现")

type Service struct {
	Writer provider.Writer
}

type Request struct {
	Source              provider.MessageRef
	MIME                []byte
	MIMEOpener          provider.MIMEOpener
	DestinationFolderID string
	Verify              bool
}

type Result struct {
	Verified bool
}

// Run 预留给 IMAP APPEND 和校验逻辑，当前只保留模块边界。
func (s *Service) Run(ctx context.Context, req Request) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}
	if len(req.MIME) == 0 && req.MIMEOpener == nil {
		return Result{}, fmt.Errorf("回写 MIME 不能为空")
	}
	if s.Writer == nil {
		return Result{}, ErrNotImplemented
	}

	result, err := s.Writer.WriteMessage(ctx, provider.WriteRequest{
		Source:              req.Source,
		MIME:                req.MIME,
		MIMEOpener:          req.MIMEOpener,
		DestinationFolderID: req.DestinationFolderID,
		Verify:              req.Verify,
	})
	if err != nil {
		if errors.Is(err, provider.ErrNotSupported) {
			return Result{}, ErrNotImplemented
		}
		return Result{}, err
	}

	return Result{Verified: result.Verified}, nil
}

// Reconcile 用于在原邮件状态不确定时，对账确认加密副本是否已存在。
func (s *Service) Reconcile(ctx context.Context, req Request) (Result, bool, error) {
	if err := req.validate(); err != nil {
		return Result{}, false, err
	}
	if s.Writer == nil {
		return Result{}, false, ErrNotImplemented
	}

	reconciler, ok := s.Writer.(provider.Reconciler)
	if !ok {
		return Result{}, false, ErrNotImplemented
	}

	result, found, err := reconciler.ReconcileMessage(ctx, provider.WriteRequest{
		Source:              req.Source,
		MIME:                req.MIME,
		MIMEOpener:          req.MIMEOpener,
		DestinationFolderID: req.DestinationFolderID,
		Verify:              req.Verify,
	})
	if err != nil {
		if errors.Is(err, provider.ErrNotSupported) {
			return Result{}, false, ErrNotImplemented
		}
		return Result{}, false, err
	}

	return Result{Verified: result.Verified}, found, nil
}

func (r Request) validate() error {
	if strings.TrimSpace(r.Source.ID) == "" {
		return fmt.Errorf("message id 不能为空")
	}
	return nil
}
