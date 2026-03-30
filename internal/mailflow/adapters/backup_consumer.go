package adapters

import (
	"context"
	"fmt"
	"strings"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/modules/audit"
	"mimecrypt/internal/modules/backup"
	"mimecrypt/internal/provider"
)

type backupAuditor interface {
	Record(event audit.Event) error
}

// BackupConsumer 将备份产物保存到本地目录。
type BackupConsumer struct {
	OutputDir string
	Service   *backup.Service
	Auditor   backupAuditor
}

func (c *BackupConsumer) Consume(_ context.Context, req mailflow.ConsumeRequest) (mailflow.DeliveryReceipt, error) {
	outputDir := strings.TrimSpace(c.OutputDir)
	if outputDir == "" {
		return mailflow.DeliveryReceipt{}, fmt.Errorf("backup output dir 不能为空")
	}

	result, err := c.service().Run(backup.Request{
		Message:          backupMessage(req.Trace),
		CiphertextOpener: req.Artifact.MIME,
		Dir:              outputDir,
	})
	if err != nil {
		return mailflow.DeliveryReceipt{}, err
	}

	if c.Auditor != nil {
		if err := c.Auditor.Record(audit.Event{
			Event:             "mailflow_backup_saved",
			MessageID:         req.Trace.SourceMessageID,
			InternetMessageID: req.Trace.InternetMessageID,
			SourceFolderID:    req.Trace.SourceFolderID,
			Format:            req.Trace.Attributes["format"],
			Encrypted:         true,
			AlreadyEncrypted:  strings.EqualFold(req.Trace.Attributes["already_encrypted"], "true"),
			BackupPath:        result.Path,
		}); err != nil {
			return mailflow.DeliveryReceipt{}, err
		}
	}

	return mailflow.DeliveryReceipt{
		Target:   req.Target.Key(),
		Consumer: req.Target.Consumer,
		ID:       result.Path,
		Store: mailflow.StoreRef{
			Driver:  "backup",
			Account: outputDir,
		},
		Verified: true,
	}, nil
}

func (c *BackupConsumer) service() *backup.Service {
	if c != nil && c.Service != nil {
		return c.Service
	}
	return &backup.Service{}
}

func backupMessage(trace mailflow.MailTrace) provider.Message {
	return provider.Message{
		ID:                firstNonEmpty(trace.SourceMessageID, trace.TransactionKey),
		InternetMessageID: trace.InternetMessageID,
		ParentFolderID:    trace.SourceFolderID,
		ReceivedDateTime:  trace.ReceivedAt,
	}
}
