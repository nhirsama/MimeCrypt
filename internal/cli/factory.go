package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/mailflow"
)

func newErrorCommand(use, short string, err error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(*cobra.Command, []string) error {
			return err
		},
	}
}

type mailflowSummary struct {
	MessageID        string
	Format           string
	Encrypted        bool
	AlreadyEncrypted bool
	SavedOutput      bool
	BackupPath       string
	WroteBack        bool
	Verified         bool
	Path             string
	Bytes            int64
}

func summarizeMailflowResult(result mailflow.Result) (mailflowSummary, error) {
	summary := mailflowSummary{
		MessageID:  strings.TrimSpace(result.Trace.SourceMessageID),
		Format:     strings.TrimSpace(result.Trace.Attributes["format"]),
		BackupPath: strings.TrimSpace(result.Trace.Attributes["backup_path"]),
	}
	if summary.MessageID == "" {
		summary.MessageID = strings.TrimSpace(result.Key)
	}
	if result.Skipped {
		summary.AlreadyEncrypted = result.Trace.Attributes["already_encrypted"] == "true"
		return summary, nil
	}
	summary.Encrypted = true

	for _, receipt := range result.Deliveries {
		switch strings.TrimSpace(receipt.Consumer) {
		case "local-output":
			summary.SavedOutput = true
			summary.Path = receipt.ID
			if summary.Path != "" {
				info, err := os.Stat(summary.Path)
				if err != nil {
					return mailflowSummary{}, fmt.Errorf("读取输出文件信息失败: %w", err)
				}
				summary.Bytes = info.Size()
			}
		case "write-back":
			summary.WroteBack = true
			summary.Verified = summary.Verified || receipt.Verified
		}
	}

	if summary.Bytes == 0 && result.Trace.Attributes != nil {
		if value := strings.TrimSpace(result.Trace.Attributes["output_bytes"]); value != "" {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return mailflowSummary{}, fmt.Errorf("解析 output bytes 失败: %w", err)
			}
			summary.Bytes = parsed
		}
	}

	return summary, nil
}

func summarizeSingleMessageResult(result mailflow.Result) (mailflowSummary, error) {
	summary, err := summarizeMailflowResult(result)
	if err != nil {
		return mailflowSummary{}, err
	}
	if summary.MessageID == "" {
		return mailflowSummary{}, fmt.Errorf("mailflow 结果缺少 message id")
	}
	return summary, nil
}
