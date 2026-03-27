package login

import (
	"context"
	"fmt"
	"io"

	"mimecrypt/internal/provider"
)

type Service struct {
	Session  provider.Session
	Mail     provider.Reader
	StateDir string
}

type Result struct {
	Account     string
	DisplayName string
	StateDir    string
}

// Run 执行登录并验证当前账号信息。
func (s *Service) Run(ctx context.Context, out io.Writer) (Result, error) {
	if _, err := s.Session.Login(ctx, out); err != nil {
		return Result{}, err
	}

	user, err := s.Mail.Me(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("登录成功，但验证当前用户信息失败: %w", err)
	}

	return Result{
		Account:     user.Account(),
		DisplayName: user.DisplayName,
		StateDir:    s.StateDir,
	}, nil
}
