package flowruntime

import (
	"path/filepath"
	"strings"
	"unicode"

	"mimecrypt/internal/appconfig"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func pushSpoolDirForSource(stateDir string, source appconfig.Source) string {
	return filepath.Join(strings.TrimSpace(stateDir), "push-spool", sanitizeRuntimeComponent(sourceRuntimeScope(source)))
}

func sourceRuntimeScope(source appconfig.Source) string {
	parts := []string{
		firstNonEmpty(source.Name, "source"),
		firstNonEmpty(source.Driver, "driver"),
	}
	if folder := strings.TrimSpace(source.Folder); folder != "" {
		parts = append(parts, folder)
	}
	return strings.Join(parts, "-")
}

func sanitizeRuntimeComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}

	var builder strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(r)
		case r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}

	result := strings.Trim(builder.String(), "._")
	if result == "" {
		return "unknown"
	}
	return result
}
