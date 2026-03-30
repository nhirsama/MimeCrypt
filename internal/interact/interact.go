package interact

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

var ErrAbort = errors.New("已取消交互配置")

func IsAbortInput(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "q", "quit", "exit":
		return true
	default:
		return false
	}
}

func PromptString(reader *bufio.Reader, out io.Writer, label, current string, allowBlank bool) (string, error) {
	for {
		prompt := label
		if strings.TrimSpace(current) != "" {
			prompt += " [" + strings.TrimSpace(current) + "]"
		}
		prompt += ": "
		if _, err := fmt.Fprint(out, prompt); err != nil {
			return "", fmt.Errorf("输出交互提示失败: %w", err)
		}
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("读取交互输入失败: %w", err)
		}
		value := strings.TrimSpace(line)
		if IsAbortInput(value) {
			return "", ErrAbort
		}
		if value == "" {
			value = strings.TrimSpace(current)
		}
		if value == "" && !allowBlank {
			if err == io.EOF {
				return "", fmt.Errorf("%s 不能为空", label)
			}
			_, _ = fmt.Fprintf(out, "%s 不能为空\n", label)
			continue
		}
		return value, nil
	}
}

func PromptInt64(reader *bufio.Reader, out io.Writer, label string, current int64) (int64, error) {
	for {
		value, err := PromptString(reader, out, label, strconv.FormatInt(current, 10), false)
		if err != nil {
			return 0, err
		}
		parsed, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if parseErr == nil && parsed >= 0 {
			return parsed, nil
		}
		_, _ = fmt.Fprintf(out, "%s 必须是非负整数\n", label)
	}
}

func PromptDuration(reader *bufio.Reader, out io.Writer, label string, current time.Duration) (time.Duration, error) {
	for {
		value, err := PromptString(reader, out, label, current.String(), false)
		if err != nil {
			return 0, err
		}
		parsed, parseErr := time.ParseDuration(strings.TrimSpace(value))
		if parseErr == nil && parsed >= 0 {
			return parsed, nil
		}
		_, _ = fmt.Fprintf(out, "%s 必须是合法 duration，例如 5m、30s\n", label)
	}
}

func PromptMenuChoice(reader *bufio.Reader, out io.Writer, label string, choices []string, current string) (string, error) {
	allowed := make(map[string]struct{}, len(choices))
	for _, choice := range choices {
		allowed[strings.ToLower(strings.TrimSpace(choice))] = struct{}{}
	}
	for {
		value, err := PromptString(reader, out, label, current, false)
		if err != nil {
			return "", err
		}
		key := strings.ToLower(strings.TrimSpace(value))
		if _, ok := allowed[key]; ok {
			return key, nil
		}
		_, _ = fmt.Fprintf(out, "无效选项: %s\n", value)
	}
}
