package logout

import (
	"fmt"

	"mimecrypt/internal/provider"
)

type Service struct {
	Session provider.Session
}

// Run 清除本地登录状态。
func (s *Service) Run() error {
	if s == nil || s.Session == nil {
		return fmt.Errorf("logout session 不能为空")
	}
	if err := s.Session.Logout(); err != nil {
		return fmt.Errorf("清除登录状态失败: %w", err)
	}
	return nil
}
