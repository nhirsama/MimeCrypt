package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

type usageError struct {
	cmd     *cobra.Command
	message string
}

func (e *usageError) Error() string {
	return e.message
}

func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		switch {
		case len(args) < n:
			return &usageError{
				cmd:     cmd,
				message: fmt.Sprintf("缺少必需参数，需要 %d 个，实际收到 %d 个", n, len(args)),
			}
		case len(args) > n:
			return &usageError{
				cmd:     cmd,
				message: fmt.Sprintf("参数过多，需要 %d 个，实际收到 %d 个", n, len(args)),
			}
		default:
			return nil
		}
	}
}

func argRange(minArgs, maxArgs int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		switch {
		case len(args) < minArgs:
			return &usageError{
				cmd:     cmd,
				message: fmt.Sprintf("缺少必需参数，至少需要 %d 个，实际收到 %d 个", minArgs, len(args)),
			}
		case len(args) > maxArgs:
			return &usageError{
				cmd:     cmd,
				message: fmt.Sprintf("参数过多，最多需要 %d 个，实际收到 %d 个", maxArgs, len(args)),
			}
		default:
			return nil
		}
	}
}

func noArgs() cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}

		return &usageError{
			cmd:     cmd,
			message: fmt.Sprintf("不需要位置参数，实际收到 %d 个", len(args)),
		}
	}
}

func newFlagUsageError(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}

	return &usageError{
		cmd:     cmd,
		message: normalizeUsageMessage(err.Error()),
	}
}

func handleUsageError(root *cobra.Command, err error) bool {
	var usageErr *usageError
	if errors.As(err, &usageErr) {
		printUsageHint(usageErr.cmd, usageErr.message)
		return true
	}

	if isGenericUsageError(err) {
		printUsageHint(root, normalizeUsageMessage(err.Error()))
		return true
	}

	return false
}

func printUsageHint(cmd *cobra.Command, message string) {
	if cmd == nil {
		return
	}

	out := cmd.ErrOrStderr()
	if out == nil {
		return
	}

	_, _ = io.WriteString(out, renderUsageHint(cmd, message))
}

func renderUsageHint(cmd *cobra.Command, message string) string {
	var builder strings.Builder
	builder.WriteString("提示: ")
	builder.WriteString(strings.TrimSpace(message))
	builder.WriteString("\n\n用法:\n  ")
	builder.WriteString(usageLine(cmd))
	builder.WriteString("\n\n使用 \"")
	builder.WriteString(cmd.CommandPath())
	builder.WriteString(" --help\" 查看帮助。\n")
	return builder.String()
}

func usageLine(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	if cmd.Parent() == nil && cmd.HasAvailableSubCommands() {
		return cmd.CommandPath() + " [command]"
	}
	return cmd.UseLine()
}

func isGenericUsageError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	patterns := []string{
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"flag needs an argument",
		"required flag",
	}
	for _, pattern := range patterns {
		if strings.Contains(message, pattern) {
			return true
		}
	}

	return false
}

func normalizeUsageMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return "命令参数不正确"
	}

	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "unknown command"):
		name := extractQuotedSegment(trimmed)
		if name != "" {
			return "未知命令: " + name
		}
		return "未知命令: " + trimmed
	case strings.HasPrefix(lower, "unknown shorthand flag:"):
		return "未知短参数: " + strings.TrimSpace(trimmed[len("unknown shorthand flag:"):])
	case strings.HasPrefix(lower, "unknown flag:"):
		return "未知参数: " + strings.TrimSpace(trimmed[len("unknown flag:"):])
	case strings.HasPrefix(lower, "flag needs an argument:"):
		return "参数缺少值: " + strings.TrimSpace(trimmed[len("flag needs an argument:"):])
	case strings.Contains(lower, "required flag"):
		return "缺少必需参数: " + trimmed
	default:
		return trimmed
	}
}

func extractQuotedSegment(value string) string {
	start := strings.IndexByte(value, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(value[start+1:], '"')
	if end < 0 {
		return ""
	}

	return value[start : start+end+2]
}
