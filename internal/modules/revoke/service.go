package revoke

import (
	"context"
	"errors"
	"fmt"
	"io"
)

type Session interface {
	Logout() error
}

type RemoteRevoker interface {
	Revoke(context.Context, io.Writer) error
}

type Service struct {
	Session          Session
	RemoteRevoker    RemoteRevoker
	ClearLocal       func() error
	Force            bool
	RequireRemote    bool
	RemotePrepareErr error
}

func (s *Service) Run(ctx context.Context, out io.Writer) error {
	if s == nil {
		return fmt.Errorf("revoke service 不能为空")
	}
	if s.Session == nil {
		return fmt.Errorf("revoke session 不能为空")
	}
	if s.ClearLocal == nil {
		return fmt.Errorf("revoke clear local 回调不能为空")
	}

	var errs []error

	if s.RequireRemote {
		if err := s.runRemote(ctx, out); err != nil {
			if !s.Force {
				return err
			}
			errs = append(errs, fmt.Errorf("远端吊销未完成，已继续清理本地凭据: %w", err))
		}
	}

	if err := s.Session.Logout(); err != nil {
		errs = append(errs, fmt.Errorf("清除本地 token 失败: %w", err))
	}
	if err := s.ClearLocal(); err != nil {
		errs = append(errs, fmt.Errorf("清除本地凭据配置失败: %w", err))
	}

	return errors.Join(errs...)
}

func (s *Service) runRemote(ctx context.Context, out io.Writer) error {
	if s.RemotePrepareErr != nil {
		return s.RemotePrepareErr
	}
	if s.RemoteRevoker == nil {
		return fmt.Errorf("远端吊销器不能为空")
	}
	if err := s.RemoteRevoker.Revoke(ctx, out); err != nil {
		return fmt.Errorf("远端吊销失败: %w", err)
	}
	return nil
}
