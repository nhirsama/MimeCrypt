package process

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/modules/audit"
	"mimecrypt/internal/modules/backup"
	"mimecrypt/internal/modules/download"
	"mimecrypt/internal/modules/encrypt"
	"mimecrypt/internal/modules/writeback"
	"mimecrypt/internal/provider"
)

type fakeDownloader struct {
	payload download.Payload
	saved   bool
	err     error
	tempDir string
}

func (f *fakeDownloader) Fetch(context.Context, string) (download.Payload, error) {
	if f.err != nil {
		return download.Payload{}, f.err
	}
	return f.payload, nil
}

func (f *fakeDownloader) FetchToTemp(_ context.Context, _ string, tempDir string) (download.TempPayload, error) {
	if f.err != nil {
		return download.TempPayload{}, f.err
	}
	f.tempDir = tempDir
	file, err := os.CreateTemp(tempDir, "payload-*.eml")
	if err != nil {
		return download.TempPayload{}, err
	}
	if _, err := file.Write(f.payload.MIME); err != nil {
		_ = file.Close()
		return download.TempPayload{}, err
	}
	if err := file.Close(); err != nil {
		return download.TempPayload{}, err
	}
	return download.TempPayload{
		Message: f.payload.Message,
		Path:    file.Name(),
		Bytes:   int64(len(f.payload.MIME)),
	}, nil
}

func (f *fakeDownloader) SavePayload(payload download.Payload, outputDir string) (download.Result, error) {
	f.saved = true
	return download.Result{
		Message: payload.Message,
		Path:    outputDir + "/encrypted.eml",
		Bytes:   int64(len(payload.MIME)),
	}, nil
}

func (f *fakeDownloader) SaveStream(message provider.Message, src io.Reader, outputDir string) (download.Result, error) {
	f.saved = true
	data, err := io.ReadAll(src)
	if err != nil {
		return download.Result{}, err
	}
	return download.Result{
		Message: message,
		Path:    outputDir + "/encrypted.eml",
		Bytes:   int64(len(data)),
	}, nil
}

type fakeEncryptor struct {
	result encrypt.Result
}

func (f fakeEncryptor) RunContext(context.Context, []byte) (encrypt.Result, error) {
	return f.result, nil
}

func (f fakeEncryptor) RunFromOpenerContext(_ context.Context, open encrypt.MIMEOpenFunc, armoredOut, mimeOut io.Writer) (encrypt.Result, error) {
	reader, err := open()
	if err != nil {
		return encrypt.Result{}, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return encrypt.Result{}, err
	}
	if armoredOut != nil {
		if _, err := armoredOut.Write(f.result.Armored); err != nil {
			return encrypt.Result{}, err
		}
	}
	if mimeOut != nil {
		if _, err := mimeOut.Write(f.result.MIME); err != nil {
			return encrypt.Result{}, err
		}
	}
	if len(data) == 0 {
		return encrypt.Result{}, errors.New("empty MIME")
	}
	return f.result, nil
}

type fakeEncryptorFunc func(context.Context, []byte) (encrypt.Result, error)

func (f fakeEncryptorFunc) RunContext(ctx context.Context, mimeBytes []byte) (encrypt.Result, error) {
	return f(ctx, mimeBytes)
}

func (f fakeEncryptorFunc) RunFromOpenerContext(ctx context.Context, open encrypt.MIMEOpenFunc, armoredOut, mimeOut io.Writer) (encrypt.Result, error) {
	reader, err := open()
	if err != nil {
		return encrypt.Result{}, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return encrypt.Result{}, err
	}
	result, err := f(ctx, data)
	if err != nil {
		return encrypt.Result{}, err
	}
	if armoredOut != nil {
		if _, err := armoredOut.Write(result.Armored); err != nil {
			return encrypt.Result{}, err
		}
	}
	if mimeOut != nil {
		if _, err := mimeOut.Write(result.MIME); err != nil {
			return encrypt.Result{}, err
		}
	}
	return result, nil
}

type fakeWriter struct {
	req             writeback.Request
	reconcileReq    writeback.Request
	reconcileResult writeback.Result
	reconcileFound  bool
	reconcileErr    error
	mime            []byte
}

func (f *fakeWriter) Run(_ context.Context, req writeback.Request) (writeback.Result, error) {
	f.req = req
	if req.MIMEOpener != nil {
		reader, err := req.MIMEOpener()
		if err != nil {
			return writeback.Result{}, err
		}
		defer reader.Close()
		f.mime, err = io.ReadAll(reader)
		if err != nil {
			return writeback.Result{}, err
		}
	} else {
		f.mime = append([]byte(nil), req.MIME...)
	}
	return writeback.Result{Verified: req.Verify}, nil
}

func (f *fakeWriter) Reconcile(_ context.Context, req writeback.Request) (writeback.Result, bool, error) {
	f.reconcileReq = req
	return f.reconcileResult, f.reconcileFound, f.reconcileErr
}

type fakeBackupper struct {
	req        backup.Request
	ciphertext []byte
}

func (f *fakeBackupper) Run(req backup.Request) (backup.Result, error) {
	f.req = req
	if req.CiphertextOpener != nil {
		reader, err := req.CiphertextOpener()
		if err != nil {
			return backup.Result{}, err
		}
		defer reader.Close()
		f.ciphertext, err = io.ReadAll(reader)
		if err != nil {
			return backup.Result{}, err
		}
	} else {
		f.ciphertext = append([]byte(nil), req.Ciphertext...)
	}
	return backup.Result{
		Path:  req.Dir + "/backup.pgp",
		Bytes: int64(len(f.ciphertext)),
	}, nil
}

type fakeAuditor struct {
	events []audit.Event
}

func (f *fakeAuditor) Record(event audit.Event) error {
	f.events = append(f.events, event)
	return nil
}

func TestRunPassesWriteBackFolders(t *testing.T) {
	t.Parallel()

	writer := &fakeWriter{}
	backupper := &fakeBackupper{}
	auditor := &fakeAuditor{}
	downloader := &fakeDownloader{
		payload: download.Payload{
			Message: provider.Message{
				ID:                "m1",
				InternetMessageID: "<m1@example.com>",
				ParentFolderID:    "source-folder",
				ReceivedDateTime:  time.Date(2026, 3, 28, 6, 32, 0, 0, time.UTC),
			},
			MIME: []byte("plain"),
		},
	}
	service := Service{
		Downloader: downloader,
		Encryptor: fakeEncryptor{
			result: encrypt.Result{
				Armored:   []byte("armored-ciphertext"),
				MIME:      []byte("encrypted-mime"),
				Encrypted: true,
				Format:    "pgp-mime",
			},
		},
		Backupper: backupper,
		WriteBack: writer,
		Auditor:   auditor,
	}

	result, err := service.Run(context.Background(), Request{
		Source:     provider.MessageRef{ID: "m1"},
		OutputDir:  "output",
		SaveOutput: true,
		BackupDir:  "backup",
		WriteBack: WriteBackOptions{
			Enabled:             true,
			DestinationFolderID: "target-folder",
			Verify:              true,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if writer.req.Source.FolderID != "source-folder" {
		t.Fatalf("SourceFolderID = %q, want source-folder", writer.req.Source.FolderID)
	}
	if writer.req.Source.InternetMessageID != "<m1@example.com>" {
		t.Fatalf("SourceInternetMessageID = %q, want <m1@example.com>", writer.req.Source.InternetMessageID)
	}
	if !writer.req.Source.ReceivedDateTime.Equal(time.Date(2026, 3, 28, 6, 32, 0, 0, time.UTC)) {
		t.Fatalf("SourceReceivedDateTime = %s", writer.req.Source.ReceivedDateTime)
	}
	if writer.req.DestinationFolderID != "target-folder" {
		t.Fatalf("DestinationFolderID = %q, want target-folder", writer.req.DestinationFolderID)
	}
	if backupper.req.Dir != "backup" {
		t.Fatalf("backup dir = %q, want backup", backupper.req.Dir)
	}
	if string(backupper.ciphertext) != "armored-ciphertext" {
		t.Fatalf("backup ciphertext = %q, want armored-ciphertext", string(backupper.ciphertext))
	}
	if !writer.req.Verify {
		t.Fatalf("Verify = false, want true")
	}
	if writer.req.MIMEOpener == nil {
		t.Fatalf("expected MIMEOpener to be passed to writeback")
	}
	if len(writer.req.MIME) != 0 {
		t.Fatalf("expected writeback MIME bytes to stay empty when using MIMEOpener")
	}
	if string(writer.mime) != "encrypted-mime" {
		t.Fatalf("writeback MIME = %q, want encrypted-mime", string(writer.mime))
	}
	if result.BackupPath != "backup/backup.pgp" {
		t.Fatalf("BackupPath = %q, want backup/backup.pgp", result.BackupPath)
	}
	if !result.WroteBack || !result.Verified {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(auditor.events) == 0 {
		t.Fatalf("expected audit events to be recorded")
	}
	if !downloader.saved {
		t.Fatalf("expected SavePayload to be called")
	}
}

func TestRunSkipsLocalSaveWhenDisabled(t *testing.T) {
	t.Parallel()

	downloader := &fakeDownloader{
		payload: download.Payload{
			Message: provider.Message{
				ID: "m1",
			},
			MIME: []byte("plain"),
		},
	}
	backupper := &fakeBackupper{}
	service := Service{
		Downloader: downloader,
		Encryptor: fakeEncryptor{
			result: encrypt.Result{
				Armored:   []byte("armored-ciphertext"),
				MIME:      []byte("encrypted-mime"),
				Encrypted: true,
				Format:    "pgp-mime",
			},
		},
		Backupper: backupper,
	}

	result, err := service.Run(context.Background(), Request{
		Source:     provider.MessageRef{ID: "m1"},
		OutputDir:  "output",
		SaveOutput: false,
		BackupDir:  "backup",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if downloader.saved {
		t.Fatalf("expected SavePayload not to be called")
	}
	if result.SavedOutput {
		t.Fatalf("SavedOutput = true, want false")
	}
	if result.Path != "" || result.Bytes != 0 {
		t.Fatalf("unexpected local save result: %+v", result)
	}
}

func TestRunUsesConfiguredWorkDirAndCleansTempDirectory(t *testing.T) {
	t.Parallel()

	workRoot := filepath.Join(t.TempDir(), "work")
	downloader := &fakeDownloader{
		payload: download.Payload{
			Message: provider.Message{ID: "m1"},
			MIME:    []byte("plain"),
		},
	}
	service := Service{
		Downloader: downloader,
		Encryptor: fakeEncryptor{
			result: encrypt.Result{
				Armored:   []byte("armored-ciphertext"),
				MIME:      []byte("encrypted-mime"),
				Encrypted: true,
				Format:    "pgp-mime",
			},
		},
		Backupper: &fakeBackupper{},
	}

	if _, err := service.Run(context.Background(), Request{
		Source:  provider.MessageRef{ID: "m1"},
		WorkDir: workRoot,
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if downloader.tempDir == "" {
		t.Fatalf("expected downloader to receive temp work dir")
	}
	wantPrefix := filepath.Clean(workRoot) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(downloader.tempDir)+string(os.PathSeparator), wantPrefix) {
		t.Fatalf("tempDir = %q, want under %q", downloader.tempDir, workRoot)
	}
	if _, err := os.Stat(workRoot); err != nil {
		t.Fatalf("Stat(workRoot) error = %v", err)
	}
	if _, err := os.Stat(downloader.tempDir); !os.IsNotExist(err) {
		t.Fatalf("tempDir still exists after cleanup: %v", err)
	}
}

func TestRunUsesCatchAllBackupEncryptorWhenConfigured(t *testing.T) {
	t.Parallel()

	backupper := &fakeBackupper{}
	service := Service{
		Downloader: &fakeDownloader{
			payload: download.Payload{
				Message: provider.Message{ID: "m1"},
				MIME:    []byte("plain"),
			},
		},
		Encryptor: fakeEncryptor{
			result: encrypt.Result{
				Armored:   []byte("message-recipient-ciphertext"),
				MIME:      []byte("encrypted-mime"),
				Encrypted: true,
				Format:    "pgp-mime",
			},
		},
		BackupEncryptor: fakeEncryptorFunc(func(context.Context, []byte) (encrypt.Result, error) {
			return encrypt.Result{
				Armored:   []byte("catch-all-backup-ciphertext"),
				Encrypted: true,
				Format:    "pgp-mime",
			}, nil
		}),
		Backupper: backupper,
	}

	if _, err := service.Run(context.Background(), Request{
		Source:    provider.MessageRef{ID: "m1"},
		BackupDir: "backup",
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if string(backupper.ciphertext) != "catch-all-backup-ciphertext" {
		t.Fatalf("backup ciphertext = %q, want catch-all-backup-ciphertext", string(backupper.ciphertext))
	}
}

func TestRunReconcilesWriteBackWhenSourceMessageIsAlreadyGone(t *testing.T) {
	t.Parallel()

	writer := &fakeWriter{
		reconcileFound:  true,
		reconcileResult: writeback.Result{Verified: true},
	}
	auditor := &fakeAuditor{}
	service := Service{
		Downloader: &fakeDownloader{
			err: errors.New("获取邮件元数据失败: Graph 请求失败: status=404 Not Found body=\"\""),
		},
		WriteBack: writer,
		Auditor:   auditor,
	}

	result, err := service.Run(context.Background(), Request{
		Source: provider.MessageRef{
			ID:                "m-gone",
			InternetMessageID: "<m-gone@example.com>",
			FolderID:          "inbox",
		},
		WriteBack: WriteBackOptions{
			Enabled:             true,
			DestinationFolderID: "archive",
			Verify:              true,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.WroteBack || !result.Verified {
		t.Fatalf("unexpected reconcile result: %+v", result)
	}
	if writer.reconcileReq.Source.InternetMessageID != "<m-gone@example.com>" {
		t.Fatalf("reconcile SourceInternetMessageID = %q", writer.reconcileReq.Source.InternetMessageID)
	}
	if writer.reconcileReq.Source.FolderID != "inbox" {
		t.Fatalf("reconcile SourceFolderID = %q", writer.reconcileReq.Source.FolderID)
	}
	if writer.reconcileReq.DestinationFolderID != "archive" {
		t.Fatalf("reconcile DestinationFolderID = %q", writer.reconcileReq.DestinationFolderID)
	}
	if len(auditor.events) != 3 {
		t.Fatalf("audit event count = %d, want 3", len(auditor.events))
	}
	if auditor.events[1].Event != "writeback_reconciled" {
		t.Fatalf("second audit event = %q, want writeback_reconciled", auditor.events[1].Event)
	}
}
