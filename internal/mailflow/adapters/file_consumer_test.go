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

func TestFileConsumerSavesMailToOutputDir(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	consumer := &FileConsumer{OutputDir: outputDir}

	result, err := consumer.Consume(context.Background(), mailflow.ConsumeRequest{
		Trace: mailflow.MailTrace{
			TransactionKey:  "tx-file",
			SourceMessageID: "msg/1",
			ReceivedAt:      time.Date(2026, 3, 29, 10, 11, 12, 0, time.UTC),
		},
		Target: mailflow.DeliveryTarget{
			Name:     "local-output",
			Consumer: "local-output",
		},
		Mail: mailflow.MailObject{
			Name: "primary",
			MIME: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("encrypted-mime")), nil
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
	content, err := os.ReadFile(result.ID)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "encrypted-mime" {
		t.Fatalf("content = %q, want encrypted-mime", string(content))
	}
}

func TestFileConsumerRequiresOutputDir(t *testing.T) {
	t.Parallel()

	consumer := &FileConsumer{}
	_, err := consumer.Consume(context.Background(), mailflow.ConsumeRequest{
		Trace: mailflow.MailTrace{TransactionKey: "tx-file-missing"},
		Target: mailflow.DeliveryTarget{
			Name:     "local-output",
			Consumer: "local-output",
		},
		Mail: mailflow.MailObject{
			Name: "primary",
			MIME: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("encrypted-mime")), nil
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "output dir 不能为空") {
		t.Fatalf("Consume() error = %v, want output dir 不能为空", err)
	}
}
