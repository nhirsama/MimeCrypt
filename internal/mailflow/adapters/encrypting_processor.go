package adapters

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/modules/audit"
	"mimecrypt/internal/modules/encrypt"
)

type MIMEEncryptor interface {
	RunFromOpenerContext(ctx context.Context, open encrypt.MIMEOpenFunc, armoredOut, mimeOut io.Writer) (encrypt.Result, error)
}

type Auditor interface {
	Record(event audit.Event) error
}

type PlanResolver func(trace mailflow.MailTrace) (mailflow.ExecutionPlan, error)

// EncryptingProcessor 在统一邮件对象上执行 MIME 加密。
type EncryptingProcessor struct {
	Encryptor   MIMEEncryptor
	Auditor     Auditor
	WorkDir     string
	StaticPlan  mailflow.ExecutionPlan
	PlanResolve PlanResolver
}

func (p *EncryptingProcessor) Process(ctx context.Context, mail mailflow.MailEnvelope) (mailflow.ProcessedMail, error) {
	if err := mail.Validate(); err != nil {
		return mailflow.ProcessedMail{}, err
	}
	if p == nil || p.Encryptor == nil {
		return mailflow.ProcessedMail{}, fmt.Errorf("encryptor 未配置")
	}

	plan, err := p.resolvePlan(mail.Trace)
	if err != nil {
		return mailflow.ProcessedMail{}, err
	}

	trace := cloneTrace(mail.Trace)
	if err := ensureAttributes(&trace); err != nil {
		return mailflow.ProcessedMail{}, err
	}
	trace.Attributes["processor"] = "mime-encrypt"

	if err := p.record(trace, "mailflow_process_started", "", "", false, false); err != nil {
		return mailflow.ProcessedMail{}, err
	}

	dir, err := os.MkdirTemp(strings.TrimSpace(p.WorkDir), "mimecrypt-mailflow-*")
	if err != nil {
		return mailflow.ProcessedMail{}, fmt.Errorf("创建 mailflow 临时目录失败: %w", err)
	}
	cleanupNow := true
	defer func() {
		if cleanupNow {
			_ = os.RemoveAll(dir)
		}
	}()

	encryptedFile, err := createTempFile(dir, "encrypted-*.eml")
	if err != nil {
		return mailflow.ProcessedMail{}, err
	}
	defer encryptedFile.Close()

	result, err := p.Encryptor.RunFromOpenerContext(ctx, encrypt.MIMEOpenFunc(mail.MIME), io.Discard, encryptedFile)
	if err != nil {
		_ = p.record(trace, "mailflow_process_failed", "", err.Error(), false, false)
		return mailflow.ProcessedMail{}, err
	}
	if err := encryptedFile.Close(); err != nil {
		return mailflow.ProcessedMail{}, fmt.Errorf("关闭 encrypted 文件失败: %w", err)
	}

	trace.Attributes["format"] = result.Format
	trace.Attributes["encrypted"] = "true"
	delete(trace.Attributes, "already_encrypted")

	if err := p.record(trace, "mailflow_process_completed", result.Format, "", result.Encrypted, result.AlreadyEncrypted); err != nil {
		return mailflow.ProcessedMail{}, err
	}

	cleanupNow = false
	return mailflow.ProcessedMail{
		Trace: trace,
		Plan:  plan,
		Mail: mailflow.MailObject{
			Name: "mail",
			MIME: func() (io.ReadCloser, error) {
				return os.Open(encryptedFile.Name())
			},
			Attributes: map[string]string{
				"format": result.Format,
			},
		},
		Cleanup: func() error {
			return os.RemoveAll(dir)
		},
	}, nil
}

func (p *EncryptingProcessor) resolvePlan(trace mailflow.MailTrace) (mailflow.ExecutionPlan, error) {
	if p != nil && p.PlanResolve != nil {
		return p.PlanResolve(trace)
	}
	if err := p.StaticPlan.Validate(); err != nil {
		return mailflow.ExecutionPlan{}, err
	}
	return p.StaticPlan, nil
}

func (p *EncryptingProcessor) record(trace mailflow.MailTrace, name, format, errText string, encrypted, alreadyEncrypted bool) error {
	if p == nil || p.Auditor == nil {
		return nil
	}
	return p.Auditor.Record(audit.Event{
		Event:             name,
		MessageID:         trace.SourceMessageID,
		InternetMessageID: trace.InternetMessageID,
		SourceFolderID:    trace.SourceFolderID,
		Format:            format,
		Encrypted:         encrypted,
		AlreadyEncrypted:  alreadyEncrypted,
		BackupPath:        trace.Attributes["backup_path"],
		Error:             errText,
	})
}

func createTempFile(dir, pattern string) (*os.File, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, fmt.Errorf("设置临时文件权限失败: %w", err)
	}
	return file, nil
}

func cloneTrace(trace mailflow.MailTrace) mailflow.MailTrace {
	cloned := trace
	if trace.Attributes != nil {
		cloned.Attributes = make(map[string]string, len(trace.Attributes))
		for key, value := range trace.Attributes {
			cloned.Attributes[key] = value
		}
	}
	return cloned
}

func ensureAttributes(trace *mailflow.MailTrace) error {
	if trace == nil {
		return fmt.Errorf("trace 不能为空")
	}
	if trace.Attributes == nil {
		trace.Attributes = make(map[string]string)
	}
	return nil
}
