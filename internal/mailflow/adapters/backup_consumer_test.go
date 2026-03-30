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
)

func TestBackupConsumerSavesArtifactToBackupDir(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	consumer := &BackupConsumer{OutputDir: outputDir}

	result, err := consumer.Consume(context.Background(), mailflow.ConsumeRequest{
		Trace: mailflow.MailTrace{
			TransactionKey:  "tx-backup",
			SourceMessageID: "msg/1",
			ReceivedAt:      time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
			Attributes: map[string]string{
				"format": "pgp-mime",
			},
		},
		Target: mailflow.DeliveryTarget{
			Name:     "backup",
			Consumer: "__default_backup__",
			Artifact: "backup",
		},
		Artifact: mailflow.MailArtifact{
			Name: "backup",
			MIME: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("backup-armored")), nil
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
}

func TestBackupConsumerRequiresOutputDir(t *testing.T) {
	t.Parallel()

	consumer := &BackupConsumer{}
	_, err := consumer.Consume(context.Background(), mailflow.ConsumeRequest{
		Trace: mailflow.MailTrace{TransactionKey: "tx-backup-missing"},
		Target: mailflow.DeliveryTarget{
			Name:     "backup",
			Consumer: "__default_backup__",
			Artifact: "backup",
		},
		Artifact: mailflow.MailArtifact{
			Name: "backup",
			MIME: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("backup-armored")), nil
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "backup output dir 不能为空") {
		t.Fatalf("Consume() error = %v, want backup output dir 不能为空", err)
	}
}
