package adapters

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/modules/audit"
	"mimecrypt/internal/modules/backup"
	"mimecrypt/internal/modules/encrypt"
	"mimecrypt/internal/provider"
)

type backupAuditor interface {
	Record(event audit.Event) error
}

type rawMIMEEncryptor interface {
	RunRawFromOpenerContext(ctx context.Context, open encrypt.MIMEOpenFunc, armoredOut io.Writer) (encrypt.Result, error)
}

// BackupConsumer 作为普通 sink consumer 接收统一邮件对象，
// 对输入 MIME 执行备份加密后保存到本地目录。
type BackupConsumer struct {
	OutputDir string
	Service   *backup.Service
	Auditor   backupAuditor
	Encryptor rawMIMEEncryptor
	WorkDir   string
}

func (c *BackupConsumer) Consume(ctx context.Context, req mailflow.ConsumeRequest) (mailflow.DeliveryReceipt, error) {
	outputDir := strings.TrimSpace(c.OutputDir)
	if outputDir == "" {
		return mailflow.DeliveryReceipt{}, fmt.Errorf("backup output dir 不能为空")
	}

	encryptor := c.encryptor()
	if encryptor == nil {
		return mailflow.DeliveryReceipt{}, fmt.Errorf("backup encryptor 未配置")
	}

	dir, err := os.MkdirTemp(strings.TrimSpace(c.WorkDir), "mimecrypt-backup-*")
	if err != nil {
		return mailflow.DeliveryReceipt{}, fmt.Errorf("创建 backup 临时目录失败: %w", err)
	}
	defer os.RemoveAll(dir)

	ciphertextFile, err := createTempFile(dir, "backup-*.pgp")
	if err != nil {
		return mailflow.DeliveryReceipt{}, err
	}
	if _, err := encryptor.RunRawFromOpenerContext(ctx, encrypt.MIMEOpenFunc(req.Mail.MIME), ciphertextFile); err != nil {
		_ = ciphertextFile.Close()
		return mailflow.DeliveryReceipt{}, fmt.Errorf("执行 backup MIME 加密失败: %w", err)
	}
	if err := ciphertextFile.Close(); err != nil {
		return mailflow.DeliveryReceipt{}, fmt.Errorf("关闭 backup 临时文件失败: %w", err)
	}

	result, err := c.service().Run(backup.Request{
		Message: backupMessage(req.Trace),
		CiphertextOpener: func() (io.ReadCloser, error) {
			return os.Open(ciphertextFile.Name())
		},
		Dir: outputDir,
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

func (c *BackupConsumer) encryptor() rawMIMEEncryptor {
	if c != nil && c.Encryptor != nil {
		return c.Encryptor
	}
	return &encrypt.Service{}
}

func backupMessage(trace mailflow.MailTrace) provider.Message {
	return provider.Message{
		ID:                firstNonEmpty(trace.SourceMessageID, trace.TransactionKey),
		InternetMessageID: trace.InternetMessageID,
		ParentFolderID:    trace.SourceFolderID,
		ReceivedDateTime:  trace.ReceivedAt,
	}
}
