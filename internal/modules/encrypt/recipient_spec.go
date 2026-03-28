package encrypt

import (
	"fmt"
	"net/mail"
	"strings"
)

func ValidateRecipientSpec(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("收件人或密钥标识不能为空")
	}
	if strings.HasPrefix(trimmed, "-") {
		return fmt.Errorf("收件人或密钥标识不能以 '-' 开头: %s", trimmed)
	}
	if strings.ContainsAny(trimmed, "\x00\r\n") {
		return fmt.Errorf("收件人或密钥标识包含非法控制字符")
	}
	return nil
}

func NormalizeEmailAddress(value string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil || strings.TrimSpace(addr.Address) == "" {
		return "", fmt.Errorf("无效的收件人邮箱: %s", strings.TrimSpace(value))
	}
	return strings.ToLower(strings.TrimSpace(addr.Address)), nil
}
