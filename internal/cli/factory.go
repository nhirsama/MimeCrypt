package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers"
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

type commandConfigBootstrap struct {
	cfg appconfig.Config
	err error
}

func loadCommandConfigBootstrap() commandConfigBootstrap {
	cfg, err := appconfig.LoadFromEnv()
	return commandConfigBootstrap{
		cfg: cfg,
		err: err,
	}
}

func (b commandConfigBootstrap) Config() appconfig.Config {
	return b.cfg
}

func (b commandConfigBootstrap) Error() error {
	return b.err
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
	summary.AlreadyEncrypted = strings.EqualFold(strings.TrimSpace(result.Trace.Attributes["already_encrypted"]), "true")
	if encrypted := strings.TrimSpace(result.Trace.Attributes["encrypted"]); encrypted != "" {
		summary.Encrypted = strings.EqualFold(encrypted, "true")
	} else {
		summary.Encrypted = summary.Format != "" && !strings.EqualFold(summary.Format, "plain")
	}
	if result.Skipped {
		return summary, nil
	}

	for _, receipt := range result.Deliveries {
		switch {
		case isBackupReceipt(receipt):
			if summary.BackupPath == "" {
				summary.BackupPath = receipt.ID
			}
		case isLocalOutputReceipt(receipt):
			summary.SavedOutput = true
			summary.Path = receipt.ID
			if summary.Path != "" {
				info, err := os.Stat(summary.Path)
				if err != nil {
					return mailflowSummary{}, fmt.Errorf("读取输出文件信息失败: %w", err)
				}
				summary.Bytes = info.Size()
			}
		case isWriteBackReceipt(receipt):
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

func isBackupReceipt(receipt mailflow.DeliveryReceipt) bool {
	driver := strings.TrimSpace(receipt.Store.Driver)
	if driver == "" {
		return false
	}
	if spec, ok := providers.LookupSinkSpec(driver); ok {
		return spec.LocalConsumer && spec.LocalConsumerKind == provider.LocalConsumerBackup
	}
	return strings.EqualFold(driver, "backup")
}

func isLocalOutputReceipt(receipt mailflow.DeliveryReceipt) bool {
	if strings.EqualFold(strings.TrimSpace(receipt.Consumer), "local-output") {
		return true
	}
	driver := strings.TrimSpace(receipt.Store.Driver)
	if driver == "" {
		return false
	}
	if spec, ok := providers.LookupSinkSpec(driver); ok {
		return spec.LocalConsumer && spec.LocalConsumerKind == provider.LocalConsumerFile
	}
	return strings.EqualFold(driver, "file")
}

func isWriteBackReceipt(receipt mailflow.DeliveryReceipt) bool {
	if strings.EqualFold(strings.TrimSpace(receipt.Consumer), "write-back") {
		return true
	}
	driver := strings.TrimSpace(receipt.Store.Driver)
	if driver == "" {
		return false
	}
	if spec, ok := providers.LookupSinkSpec(driver); ok {
		return !spec.LocalConsumer
	}
	return !strings.EqualFold(driver, "file")
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
