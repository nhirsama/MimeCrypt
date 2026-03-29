package login

import (
	"context"
	"fmt"
	"io"

	"mimecrypt/internal/provider"
)

type Service struct {
	Session       provider.Session
	IdentityProbe func(context.Context) (provider.User, error)
	StateDir      string
}

type Result struct {
	Account     string
	DisplayName string
	StateDir    string
}

// Run 执行登录流程，并读取登录账号信息用于校验。
func (s *Service) Run(ctx context.Context, out io.Writer) (Result, error) {
	if _, err := s.Session.Login(ctx, out); err != nil {
		return Result{}, err
	}

	user := provider.User{}
	if s.IdentityProbe != nil {
		var err error
		user, err = s.IdentityProbe(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("登录成功，但验证当前用户信息失败: %w", err)
		}
	}

	return Result{
		Account:     user.Account(),
		DisplayName: user.DisplayName,
		StateDir:    s.StateDir,
	}, nil
}
