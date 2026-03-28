package cli

import (
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/modules/encrypt"
)

const (
	envPGPRecipientsKey = "MIMECRYPT_PGP_RECIPIENTS"
	envGPGBinaryKey     = "MIMECRYPT_GPG_BINARY"
)

func newEncryptCmd() *cobra.Command {
	var recipients []string
	var keys []string
	var gpgBinary string
	var protectSubject bool

	cmd := &cobra.Command{
		Use:   "encrypt <input.eml> <output.eml>",
		Short: "将本地 MIME 文件转换为 PGP/MIME",
		Args:  exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			inputPath := strings.TrimSpace(args[0])
			outputPath := strings.TrimSpace(args[1])
			if inputPath == "" {
				return fmt.Errorf("encrypt 失败: input 不能为空")
			}
			if outputPath == "" {
				return fmt.Errorf("encrypt 失败: output 不能为空")
			}

			input, err := os.ReadFile(inputPath)
			if err != nil {
				return fmt.Errorf("encrypt 失败: 读取输入文件失败: %w", err)
			}

			service, err := buildLocalEncryptService(recipients, keys, gpgBinary, protectSubject)
			if err != nil {
				return fmt.Errorf("encrypt 失败: %w", err)
			}

			result, err := service.RunContext(cmd.Context(), input)
			if err != nil {
				return fmt.Errorf("encrypt 失败: %w", err)
			}

			if err := writeSecureFile(outputPath, result.MIME, 0o600); err != nil {
				return fmt.Errorf("encrypt 失败: 写入输出文件失败: %w", err)
			}

			fmt.Printf("加密完成，format=%s encrypted=%t output=%s bytes=%d\n", result.Format, result.Encrypted, outputPath, len(result.MIME))
			return nil
		},
	}

	cmd.Flags().StringArrayVarP(&recipients, "recipient", "r", nil, "指定加密收件人邮箱，可重复")
	cmd.Flags().StringArrayVar(&keys, "key", nil, "指定 GPG key（指纹、key id 或 user id），可重复")
	cmd.Flags().StringVar(&gpgBinary, "gpg-binary", "", "gpg 可执行文件路径，覆盖 MIMECRYPT_GPG_BINARY")
	cmd.Flags().BoolVar(&protectSubject, "protect-subject", false, "将外层邮件主题写为 \"...\"")

	return cmd
}

func buildLocalEncryptService(explicitRecipients, explicitKeys []string, gpgBinary string, protectSubject bool) (*encrypt.Service, error) {
	trimmedBinary := strings.TrimSpace(gpgBinary)
	recipients, err := normalizeExplicitRecipientEmails(explicitRecipients)
	if err != nil {
		return nil, err
	}
	keys, err := normalizeExplicitKeySpecs(explicitKeys)
	if err != nil {
		return nil, err
	}
	specs := append(append([]string(nil), recipients...), keys...)
	service := &encrypt.Service{ProtectSubject: protectSubject}

	if len(specs) > 0 {
		recipientCopy := append([]string(nil), specs...)
		service.RecipientResolver = func([]byte) ([]string, error) {
			return recipientCopy, nil
		}
	}

	if len(specs) > 0 || trimmedBinary != "" {
		service.EnvLookup = func(key string) string {
			switch key {
			case envPGPRecipientsKey:
				if len(specs) > 0 {
					return strings.Join(specs, ",")
				}
			case envGPGBinaryKey:
				if trimmedBinary != "" {
					return trimmedBinary
				}
			}
			return os.Getenv(key)
		}
	}

	return service, nil
}

func normalizeExplicitRecipientEmails(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))

	for _, value := range values {
		for _, part := range splitRecipientSpec(value) {
			email, err := normalizeExplicitRecipientEmail(part)
			if err != nil {
				return nil, err
			}
			if email == "" {
				continue
			}
			if _, ok := seen[email]; ok {
				continue
			}
			seen[email] = struct{}{}
			out = append(out, email)
		}
	}

	return out, nil
}

func normalizeExplicitKeySpecs(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))

	for _, value := range values {
		for _, part := range splitRecipientSpec(value) {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			if err := encrypt.ValidateRecipientSpec(trimmed); err != nil {
				return nil, err
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			out = append(out, trimmed)
		}
	}

	return out, nil
}

func normalizeExplicitRecipientEmail(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil || strings.TrimSpace(addr.Address) == "" {
		return "", fmt.Errorf("无效的收件人邮箱: %s", trimmed)
	}
	return strings.ToLower(strings.TrimSpace(addr.Address)), nil
}

func splitRecipientSpec(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	if !strings.ContainsAny(trimmed, ",;\n\r") {
		return []string{trimmed}
	}

	return strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
}

func writeSecureFile(path string, content []byte, mode os.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("输出路径不能为空")
	}

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}

	return os.WriteFile(path, content, mode)
}
