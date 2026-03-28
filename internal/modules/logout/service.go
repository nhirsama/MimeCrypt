package logout

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Service struct {
	TokenPaths []string
}

// Run 清除本地登录状态。
func (s *Service) Run() error {
	tokenPaths := uniqueNonEmpty(s.TokenPaths)
	if len(tokenPaths) == 0 {
		return fmt.Errorf("token 路径不能为空")
	}

	for _, tokenPath := range tokenPaths {
		if err := os.Remove(tokenPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("删除 token 缓存失败: %w", err)
		}
	}

	return nil
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
