package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRootHelpIsLocalizedToChinese(t *testing.T) {
	t.Parallel()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})

	if err := executeRootCommand(context.Background(), root); err != nil {
		t.Fatalf("executeRootCommand() error = %v", err)
	}

	rendered := out.String()
	for _, fragment := range []string{
		"用法:",
		"可用命令:",
		"completion  生成指定 shell 的自动补全脚本",
		"help        查看任意命令的帮助",
		`-h, --help   查看帮助`,
		`使用 "mimecrypt [command] --help" 查看更多信息。`,
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("help output missing %q:\n%s", fragment, rendered)
		}
	}
	for _, fragment := range []string{
		"Usage:",
		"Available Commands:",
		"Help about any command",
	} {
		if strings.Contains(rendered, fragment) {
			t.Fatalf("help output unexpectedly contains %q:\n%s", fragment, rendered)
		}
	}
}

func TestCompletionHelpIsLocalizedToChinese(t *testing.T) {
	t.Parallel()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"completion", "zsh", "--help"})

	if err := executeRootCommand(context.Background(), root); err != nil {
		t.Fatalf("executeRootCommand() error = %v", err)
	}

	rendered := out.String()
	for _, fragment := range []string{
		"为 zsh 生成自动补全脚本。",
		"如果当前环境尚未启用 shell 补全，需要先启用一次：",
		"在当前 shell 会话中加载补全：",
		"为每个新会话启用补全，执行一次：",
		"--no-descriptions",
		"禁用补全说明",
		"-h, --help",
		"查看帮助",
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("completion help missing %q:\n%s", fragment, rendered)
		}
	}
	for _, fragment := range []string{
		"Generate the autocompletion script",
		"If shell completion is not already enabled",
		"disable completion descriptions",
	} {
		if strings.Contains(rendered, fragment) {
			t.Fatalf("completion help unexpectedly contains %q:\n%s", fragment, rendered)
		}
	}
}
