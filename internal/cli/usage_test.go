package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExecuteRootCommandShowsHintForMissingArgs(t *testing.T) {
	t.Parallel()

	root := newRootCmd()
	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetArgs([]string{"encrypt", "input.eml"})

	if err := executeRootCommand(context.Background(), root); err != nil {
		t.Fatalf("executeRootCommand() error = %v", err)
	}

	output := stderr.String()
	if !strings.Contains(output, "提示: 缺少必需参数，应提供 2 个，实际收到 1 个") {
		t.Fatalf("expected hint message, got %q", output)
	}
	if !strings.Contains(output, "mimecrypt encrypt <input.eml> <output.eml>") {
		t.Fatalf("expected usage line, got %q", output)
	}
}

func TestExecuteRootCommandShowsHintForUnknownFlag(t *testing.T) {
	t.Parallel()

	root := newRootCmd()
	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetArgs([]string{"encrypt", "--bad-flag"})

	if err := executeRootCommand(context.Background(), root); err != nil {
		t.Fatalf("executeRootCommand() error = %v", err)
	}

	output := stderr.String()
	if !strings.Contains(output, "提示: 未知参数: --bad-flag") {
		t.Fatalf("expected flag hint, got %q", output)
	}
	if !strings.Contains(output, "mimecrypt encrypt <input.eml> <output.eml>") {
		t.Fatalf("expected encrypt usage line, got %q", output)
	}
}

func TestExecuteRootCommandShowsHintForUnknownCommand(t *testing.T) {
	t.Parallel()

	root := newRootCmd()
	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetArgs([]string{"unknown"})

	if err := executeRootCommand(context.Background(), root); err != nil {
		t.Fatalf("executeRootCommand() error = %v", err)
	}

	output := stderr.String()
	if !strings.Contains(output, "提示: 未知命令: \"unknown\"") {
		t.Fatalf("expected unknown command hint, got %q", output)
	}
	if !strings.Contains(output, "mimecrypt [command]") {
		t.Fatalf("expected root usage line, got %q", output)
	}
}

func TestNoArgsRejectsUnexpectedPositionals(t *testing.T) {
	t.Parallel()

	cmd := newLoginCmd()
	err := noArgs()(cmd, []string{"extra"})
	if err == nil || !strings.Contains(err.Error(), "该命令不接受位置参数") {
		t.Fatalf("expected noArgs error, got %v", err)
	}
}
