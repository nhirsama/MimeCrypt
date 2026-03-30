package adapters

import (
	"context"
	"errors"
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

// EncryptingProcessor 复用现有加密、备份与审计模块，将原始邮件转换为 mailflow.ProcessedMail。
type EncryptingProcessor struct {
	Encryptor       MIMEEncryptor
	BackupEncryptor MIMEEncryptor
	Auditor         Auditor
	WorkDir         string
	StaticPlan      mailflow.ExecutionPlan
	PlanResolve     PlanResolver
}

const backupArtifactName = "backup"

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

	armoredFile, err := createTempFile(dir, "armored-*.asc")
	if err != nil {
		return mailflow.ProcessedMail{}, err
	}
	defer armoredFile.Close()

	encryptedFile, err := createTempFile(dir, "encrypted-*.eml")
	if err != nil {
		return mailflow.ProcessedMail{}, err
	}
	defer encryptedFile.Close()

	result, err := p.Encryptor.RunFromOpenerContext(ctx, encrypt.MIMEOpenFunc(mail.MIME), armoredFile, encryptedFile)
	if err != nil {
		if errors.Is(err, encrypt.ErrAlreadyEncrypted) {
			if ensureErr := ensureAttributes(&trace); ensureErr != nil {
				return mailflow.ProcessedMail{}, ensureErr
			}
			trace.Attributes["already_encrypted"] = "true"
			var alreadyEncrypted encrypt.AlreadyEncryptedError
			if errors.As(err, &alreadyEncrypted) && strings.TrimSpace(alreadyEncrypted.Format) != "" {
				trace.Attributes["format"] = alreadyEncrypted.Format
			}
			_ = p.record(trace, "mailflow_already_encrypted", "", err.Error(), false, true)
			return mailflow.ProcessedMail{}, mailflow.NewSkipError(trace, err)
		}
		_ = p.record(trace, "mailflow_process_failed", "", err.Error(), false, false)
		return mailflow.ProcessedMail{}, err
	}

	if err := armoredFile.Close(); err != nil {
		return mailflow.ProcessedMail{}, fmt.Errorf("关闭 armored 文件失败: %w", err)
	}
	if err := encryptedFile.Close(); err != nil {
		return mailflow.ProcessedMail{}, fmt.Errorf("关闭 encrypted 文件失败: %w", err)
	}

	if err := ensureAttributes(&trace); err != nil {
		return mailflow.ProcessedMail{}, err
	}
	trace.Attributes["format"] = result.Format

	if err := p.record(trace, "mailflow_process_completed", result.Format, "", result.Encrypted, result.AlreadyEncrypted); err != nil {
		return mailflow.ProcessedMail{}, err
	}

	artifacts := map[string]mailflow.MailArtifact{
		"primary": {
			Name: "primary",
			MIME: func() (io.ReadCloser, error) {
				return os.Open(encryptedFile.Name())
			},
			Attributes: map[string]string{
				"format": result.Format,
			},
		},
	}
	if needsBackupArtifact(plan) {
		backupPath := armoredFile.Name()
		if p.BackupEncryptor != nil {
			backupFile, err := createTempFile(dir, "backup-*.pgp")
			if err != nil {
				_ = p.record(trace, "mailflow_process_failed", result.Format, err.Error(), result.Encrypted, result.AlreadyEncrypted)
				return mailflow.ProcessedMail{}, err
			}
			if _, err := p.BackupEncryptor.RunFromOpenerContext(ctx, encrypt.MIMEOpenFunc(mail.MIME), backupFile, nil); err != nil {
				_ = backupFile.Close()
				_ = p.record(trace, "mailflow_process_failed", result.Format, err.Error(), result.Encrypted, result.AlreadyEncrypted)
				return mailflow.ProcessedMail{}, err
			}
			if err := backupFile.Close(); err != nil {
				_ = p.record(trace, "mailflow_process_failed", result.Format, err.Error(), result.Encrypted, result.AlreadyEncrypted)
				return mailflow.ProcessedMail{}, fmt.Errorf("关闭 backup 文件失败: %w", err)
			}
			backupPath = backupFile.Name()
		}
		artifacts[backupArtifactName] = mailflow.MailArtifact{
			Name: backupArtifactName,
			MIME: func() (io.ReadCloser, error) {
				return os.Open(backupPath)
			},
			Attributes: map[string]string{
				"format": "pgp",
			},
		}
	}

	cleanupNow = false
	return mailflow.ProcessedMail{
		Trace:     trace,
		Plan:      plan,
		Artifacts: artifacts,
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

func needsBackupArtifact(plan mailflow.ExecutionPlan) bool {
	for _, target := range plan.Targets {
		if strings.EqualFold(strings.TrimSpace(target.Artifact), backupArtifactName) {
			return true
		}
	}
	return false
}
