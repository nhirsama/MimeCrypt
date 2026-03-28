package encrypt

import (
	"bytes"
	"fmt"
	"net/mail"
	"slices"
	"strings"
)

var recipientHeaderKeys = []string{"To", "Cc", "Bcc"}

func (s *Service) resolveRecipients(mimeBytes []byte) ([]string, error) {
	recipients := collectRecipientsFromEnv(s.getenv(envPGPRecipients))

	message, err := mail.ReadMessage(bytes.NewReader(mimeBytes))
	if err != nil {
		if len(recipients) == 0 {
			return nil, fmt.Errorf("解析 MIME 头失败: %w", err)
		}
		return dedupeRecipients(recipients), nil
	}

	for _, key := range recipientHeaderKeys {
		values := headerValues(message.Header, key)
		for _, raw := range values {
			recipients = append(recipients, parseAddressList(raw)...)
		}
	}

	return dedupeRecipients(recipients), nil
}

func collectRecipientsFromEnv(value string) []string {
	return parseAddressList(strings.NewReplacer(";", ",", "\n", ",").Replace(value))
}

func parseAddressList(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	addresses, err := mail.ParseAddressList(trimmed)
	if err == nil {
		result := make([]string, 0, len(addresses))
		for _, addr := range addresses {
			email := strings.TrimSpace(strings.ToLower(addr.Address))
			if email != "" {
				result = append(result, email)
			}
		}
		return result
	}

	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		email, normalizeErr := NormalizeEmailAddress(part)
		if normalizeErr == nil {
			result = append(result, email)
		}
	}

	return result
}

func dedupeRecipients(recipients []string) []string {
	seen := make(map[string]struct{}, len(recipients))
	result := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		key := strings.TrimSpace(strings.ToLower(recipient))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}

	slices.Sort(result)
	return result
}
