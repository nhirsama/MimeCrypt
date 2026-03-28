package process

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"mimecrypt/internal/modules/audit"
	"mimecrypt/internal/modules/backup"
	"mimecrypt/internal/modules/download"
	"mimecrypt/internal/modules/encrypt"
	"mimecrypt/internal/modules/writeback"
	"mimecrypt/internal/provider"
)

type Downloader interface {
	Fetch(ctx context.Context, messageID string) (download.Payload, error)
	SavePayload(payload download.Payload, outputDir string) (download.Result, error)
}

type Encryptor interface {
	RunContext(ctx context.Context, mimeBytes []byte) (encrypt.Result, error)
}

type Backupper interface {
	Run(req backup.Request) (backup.Result, error)
}

type Writer interface {
	Run(ctx context.Context, req writeback.Request) (writeback.Result, error)
}

type ReconcilingWriter interface {
	Reconcile(ctx context.Context, req writeback.Request) (writeback.Result, bool, error)
}

type Auditor interface {
	Record(event audit.Event) error
}

type Service struct {
	Downloader      Downloader
	Encryptor       Encryptor
	BackupEncryptor Encryptor
	Backupper       Backupper
	WriteBack       Writer
	Auditor         Auditor
}

type WriteBackOptions struct {
	Enabled             bool
	DestinationFolderID string
	Verify              bool
}

type Request struct {
	Source     provider.MessageRef
	OutputDir  string
	SaveOutput bool
	BackupDir  string
	WriteBack  WriteBackOptions
}

type Result struct {
	MessageID        string
	Path             string
	Bytes            int64
	Encrypted        bool
	AlreadyEncrypted bool
	Format           string
	SavedOutput      bool
	BackupPath       string
	WroteBack        bool
	Verified         bool
}

type runState struct {
	service       *Service
	request       Request
	source        provider.MessageRef
	payload       download.Payload
	encryptResult encrypt.Result
	backupResult  backup.Result
	result        Result
}

// Run 根据邮件 ID 和配置处理邮件。
func (s *Service) Run(ctx context.Context, req Request) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}

	run := runState{
		service: s,
		request: req,
		source:  req.Source,
		result: Result{
			MessageID: req.Source.ID,
		},
	}

	return run.execute(ctx)
}

func (r Request) validate() error {
	if strings.TrimSpace(r.Source.ID) == "" {
		return fmt.Errorf("message id 不能为空")
	}
	return nil
}

func (r *runState) execute(ctx context.Context) (Result, error) {
	if err := r.record("process_started"); err != nil {
		return Result{}, err
	}

	loaded, err := r.loadPayload(ctx)
	if err != nil {
		return Result{}, err
	}
	if !loaded {
		return r.result, nil
	}

	if err := r.encrypt(ctx); err != nil {
		return Result{}, err
	}
	if err := r.backup(ctx); err != nil {
		return Result{}, err
	}
	if err := r.saveOutput(); err != nil {
		return Result{}, err
	}
	if err := r.writeBack(ctx); err != nil {
		return Result{}, err
	}
	if err := r.record("process_completed"); err != nil {
		return Result{}, err
	}

	return r.result, nil
}

func (r *runState) loadPayload(ctx context.Context) (bool, error) {
	payload, err := r.service.Downloader.Fetch(ctx, r.source.ID)
	if err != nil {
		reconciled, reconcileErr := r.reconcileFetchFailure(ctx)
		if reconcileErr != nil {
			return false, fmt.Errorf("%w；回写对账失败: %v", err, reconcileErr)
		}
		if reconciled {
			return false, nil
		}
		return false, r.fail("fetch_failed", err)
	}

	r.payload = payload
	r.source = payload.Message.Ref()
	r.result.MessageID = r.source.ID

	return true, r.record("fetched")
}

func (r *runState) encrypt(ctx context.Context) error {
	encryptResult, err := r.service.Encryptor.RunContext(ctx, r.payload.MIME)
	if err != nil {
		if errors.Is(err, encrypt.ErrAlreadyEncrypted) {
			event := r.event("already_encrypted")
			event.AlreadyEncrypted = true
			event.Error = err.Error()
			return r.service.fail(event, err)
		}
		return r.fail("encrypt_failed", err)
	}

	r.encryptResult = encryptResult
	r.result.Encrypted = encryptResult.Encrypted
	r.result.AlreadyEncrypted = encryptResult.AlreadyEncrypted
	r.result.Format = encryptResult.Format

	return r.record("encrypted")
}

func (r *runState) backup(ctx context.Context) error {
	backupCiphertext := r.encryptResult.Armored
	if r.service.BackupEncryptor != nil {
		backupEncryptResult, err := r.service.BackupEncryptor.RunContext(ctx, r.payload.MIME)
		if err != nil {
			return r.fail("backup_encrypt_failed", err)
		}
		backupCiphertext = backupEncryptResult.Armored
	}

	backupDir := r.request.BackupDir
	if backupDir == "" {
		backupDir = r.request.OutputDir
	}

	backupResult, err := r.service.backupper().Run(backup.Request{
		Message:    r.payload.Message,
		Ciphertext: backupCiphertext,
		Dir:        backupDir,
	})
	if err != nil {
		return r.fail("backup_failed", err)
	}

	r.backupResult = backupResult
	r.result.BackupPath = backupResult.Path

	return r.record("backup_saved")
}

func (r *runState) saveOutput() error {
	if !r.request.SaveOutput {
		return nil
	}

	payload := r.payload
	payload.MIME = r.encryptResult.MIME

	saveResult, err := r.service.Downloader.SavePayload(payload, r.request.OutputDir)
	if err != nil {
		return r.fail("mime_save_failed", err)
	}

	r.result.SavedOutput = true
	r.result.Path = saveResult.Path
	r.result.Bytes = saveResult.Bytes

	return nil
}

func (r *runState) writeBack(ctx context.Context) error {
	if !r.request.WriteBack.Enabled || r.service.WriteBack == nil {
		return nil
	}

	writeBackResult, err := r.service.WriteBack.Run(ctx, writeback.Request{
		Source:              r.source,
		MIME:                r.encryptResult.MIME,
		DestinationFolderID: r.request.WriteBack.DestinationFolderID,
		Verify:              r.request.WriteBack.Verify,
	})
	if err != nil {
		return r.fail("writeback_failed", err)
	}

	r.result.WroteBack = true
	r.result.Verified = writeBackResult.Verified

	return r.record("writeback_succeeded")
}

func (r *runState) reconcileFetchFailure(ctx context.Context) (bool, error) {
	if !r.request.WriteBack.Enabled || r.service == nil || r.service.WriteBack == nil {
		return false, nil
	}
	if r.source.InternetMessageID == "" {
		return false, nil
	}

	reconciler, ok := r.service.WriteBack.(ReconcilingWriter)
	if !ok {
		return false, nil
	}

	writeBackResult, found, err := reconciler.Reconcile(ctx, writeback.Request{
		Source:              r.source,
		DestinationFolderID: r.request.WriteBack.DestinationFolderID,
		Verify:              r.request.WriteBack.Verify,
	})
	if err != nil {
		if errors.Is(err, writeback.ErrNotImplemented) {
			return false, nil
		}
		return false, err
	}
	if !found {
		return false, nil
	}

	r.result.MessageID = r.source.ID
	r.result.WroteBack = true
	r.result.Verified = writeBackResult.Verified

	if err := r.record("writeback_reconciled"); err != nil {
		return false, err
	}
	if err := r.record("process_completed"); err != nil {
		return false, err
	}

	return true, nil
}

func (r *runState) event(name string) audit.Event {
	return audit.Event{
		Event:             name,
		MessageID:         r.source.ID,
		InternetMessageID: r.source.InternetMessageID,
		SourceFolderID:    r.source.FolderID,
		DestinationFolder: r.request.WriteBack.DestinationFolderID,
		Format:            r.result.Format,
		Encrypted:         r.result.Encrypted,
		AlreadyEncrypted:  r.result.AlreadyEncrypted,
		BackupPath:        r.result.BackupPath,
		WroteBack:         r.result.WroteBack,
		Verified:          r.result.Verified,
	}
}

func (r *runState) record(name string) error {
	return r.service.record(r.event(name))
}

func (r *runState) fail(name string, err error) error {
	event := r.event(name)
	event.Error = err.Error()
	return r.service.fail(event, err)
}

func (s *Service) backupper() Backupper {
	if s != nil && s.Backupper != nil {
		return s.Backupper
	}
	return &backup.Service{}
}

func (s *Service) record(event audit.Event) error {
	if s == nil || s.Auditor == nil {
		return nil
	}
	return s.Auditor.Record(event)
}

func (s *Service) fail(event audit.Event, err error) error {
	if logErr := s.record(event); logErr != nil {
		return fmt.Errorf("%w; 记录审计日志失败: %w", err, logErr)
	}
	return err
}
