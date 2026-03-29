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
	DeleteSource        bool
}

type Result struct {
	Verified bool
}

// Run 执行邮件回写，并将协议细节委托给底层 provider.Writer。
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
		DeleteSource:        req.DeleteSource,
	})
	if err != nil {
		if errors.Is(err, provider.ErrNotSupported) {
			return Result{}, ErrNotImplemented
		}
		return Result{}, err
	}

	return Result{Verified: result.Verified}, nil
}

// Reconcile 在原邮件状态不确定时执行对账，确认目标邮件是否已经存在。
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
		DeleteSource:        req.DeleteSource,
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
	if r.DeleteSource && strings.TrimSpace(r.Source.ID) == "" {
		return fmt.Errorf("message id 不能为空")
	}
	return nil
}
