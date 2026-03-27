package logout

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Service struct {
	TokenPath string
}

// Run 清除本地登录状态。
func (s *Service) Run() error {
	if strings.TrimSpace(s.TokenPath) == "" {
		return fmt.Errorf("token 路径不能为空")
	}

	if err := os.Remove(s.TokenPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("删除 token 缓存失败: %w", err)
	}

	return nil
}
