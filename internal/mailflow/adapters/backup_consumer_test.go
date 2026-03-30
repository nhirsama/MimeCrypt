package adapters

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/modules/encrypt"
)

type fakeRawMailEncryptor struct {
	output string
	err    error
	input  string
}

func (e *fakeRawMailEncryptor) RunRawFromOpenerContext(_ context.Context, open encrypt.MIMEOpenFunc, out io.Writer) (encrypt.Result, error) {
	if e.err != nil {
		return encrypt.Result{}, e.err
	}
	reader, err := open()
	if err != nil {
		return encrypt.Result{}, err
	}
	defer reader.Close()
	input, err := io.ReadAll(reader)
	if err != nil {
		return encrypt.Result{}, err
	}
	e.input = string(input)
	if _, err := out.Write([]byte(e.output)); err != nil {
		return encrypt.Result{}, err
	}
	return encrypt.Result{Encrypted: true, Format: "pgp"}, nil
}

func TestBackupConsumerEncryptsAndSavesMailToBackupDir(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	encryptor := &fakeRawMailEncryptor{output: "backup-armored"}
	consumer := &BackupConsumer{
		OutputDir: outputDir,
		Encryptor: encryptor,
	}

	result, err := consumer.Consume(context.Background(), mailflow.ConsumeRequest{
		Trace: mailflow.MailTrace{
			TransactionKey:  "tx-backup",
			SourceMessageID: "msg/1",
			ReceivedAt:      time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
			Attributes: map[string]string{
				"format": "pgp-mime",
			},
		},
		Target: mailflow.DeliveryTarget{Name: "backup", Consumer: "backup", Artifact: "backup"},
		Mail: mailflow.MailObject{
			Name: "message",
			MIME: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("source-mime")), nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if !result.Verified {
		t.Fatalf("Verified = false, want true")
	}
	if filepath.Dir(result.ID) != outputDir {
		t.Fatalf("output path = %q, want within %q", result.ID, outputDir)
	}
	if filepath.Ext(result.ID) != ".pgp" {
		t.Fatalf("path ext = %q, want .pgp", filepath.Ext(result.ID))
	}
	content, err := os.ReadFile(result.ID)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "backup-armored" {
		t.Fatalf("content = %q, want backup-armored", string(content))
	}
	if encryptor.input != "source-mime" {
		t.Fatalf("backup input MIME = %q, want source-mime", encryptor.input)
	}
}

func TestBackupConsumerRequiresOutputDir(t *testing.T) {
	t.Parallel()

	consumer := &BackupConsumer{Encryptor: &fakeRawMailEncryptor{output: "backup-armored"}}
	_, err := consumer.Consume(context.Background(), mailflow.ConsumeRequest{
		Trace:  mailflow.MailTrace{TransactionKey: "tx-backup-missing"},
		Target: mailflow.DeliveryTarget{Name: "backup", Consumer: "backup", Artifact: "backup"},
		Mail: mailflow.MailObject{
			Name: "message",
			MIME: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("source-mime")), nil
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "backup output dir 不能为空") {
		t.Fatalf("Consume() error = %v, want backup output dir 不能为空", err)
	}
}
