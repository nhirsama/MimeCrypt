package adapters

import (
	"context"
	"fmt"
	"strings"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/mimefile"
	"mimecrypt/internal/provider"
)

// FileConsumer 将处理后的 MIME 追加保存到本地目录。
type FileConsumer struct {
	OutputDir string
}

func (c *FileConsumer) Consume(_ context.Context, req mailflow.ConsumeRequest) (mailflow.DeliveryReceipt, error) {
	outputDir := strings.TrimSpace(c.OutputDir)
	if outputDir == "" {
		return mailflow.DeliveryReceipt{}, fmt.Errorf("output dir 不能为空")
	}

	reader, err := req.Mail.MIME()
	if err != nil {
		return mailflow.DeliveryReceipt{}, fmt.Errorf("打开邮件对象 MIME 失败: %w", err)
	}
	defer reader.Close()

	path, _, err := mimefile.SaveToOutputDir(outputDir, provider.Message{
		ID:               firstNonEmpty(req.Trace.SourceMessageID, req.Trace.TransactionKey),
		ReceivedDateTime: req.Trace.ReceivedAt,
	}, reader)
	if err != nil {
		return mailflow.DeliveryReceipt{}, err
	}

	return mailflow.DeliveryReceipt{
		Target:   req.Target.Key(),
		Consumer: req.Target.Consumer,
		ID:       path,
		Store: mailflow.StoreRef{
			Driver:  "file",
			Account: outputDir,
		},
		Verified: true,
	}, nil
}
