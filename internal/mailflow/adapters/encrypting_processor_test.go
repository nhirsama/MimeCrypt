package adapters

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/modules/audit"
	"mimecrypt/internal/modules/encrypt"
)

type fakeMailEncryptor struct {
	result        encrypt.Result
	err           error
	armoredOutput string
	mimeOutput    string
}

func (e *fakeMailEncryptor) RunFromOpenerContext(_ context.Context, open encrypt.MIMEOpenFunc, armoredOut, mimeOut io.Writer) (encrypt.Result, error) {
	if e.err != nil {
		return encrypt.Result{}, e.err
	}
	reader, err := open()
	if err != nil {
		return encrypt.Result{}, err
	}
	defer reader.Close()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return encrypt.Result{}, err
	}
	if armoredOut != nil {
		output := e.armoredOutput
		if output == "" {
			output = "armored"
		}
		if _, err := armoredOut.Write([]byte(output)); err != nil {
			return encrypt.Result{}, err
		}
	}
	if mimeOut != nil {
		output := e.mimeOutput
		if output == "" {
			output = "encrypted-mime"
		}
		if _, err := mimeOut.Write([]byte(output)); err != nil {
			return encrypt.Result{}, err
		}
	}
	return e.result, nil
}

type fakeAuditor struct {
	events []audit.Event
}

func (a *fakeAuditor) Record(event audit.Event) error {
	a.events = append(a.events, event)
	return nil
}

func TestEncryptingProcessorBuildsProcessedMail(t *testing.T) {
	t.Parallel()

	auditor := &fakeAuditor{}
	processor := &EncryptingProcessor{
		Encryptor: &fakeMailEncryptor{
			result: encrypt.Result{Encrypted: true, Format: "pgp-mime"},
		},
		Auditor: auditor,
		StaticPlan: mailflow.ExecutionPlan{
			Targets: []mailflow.DeliveryTarget{{
				Name:     "archive-main",
				Consumer: "archive",
				Artifact: "primary",
				Required: true,
			}},
		},
	}

	result, err := processor.Process(context.Background(), mailflow.MailEnvelope{
		MIME: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("source-mime")), nil
		},
		Trace: mailflow.MailTrace{
			TransactionKey:    "tx-processor",
			SourceMessageID:   "m1",
			SourceFolderID:    "INBOX",
			InternetMessageID: "<m1@example.com>",
		},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Trace.Attributes["format"] != "pgp-mime" {
		t.Fatalf("format attr = %q, want pgp-mime", result.Trace.Attributes["format"])
	}
	reader, err := result.Mail.MIME()
	if err != nil {
		t.Fatalf("mail MIME() error = %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	_ = reader.Close()
	if string(data) != "encrypted-mime" {
		t.Fatalf("mail data = %q, want encrypted-mime", string(data))
	}
	if len(auditor.events) == 0 {
		t.Fatalf("expected audit events")
	}
	if err := result.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := result.Mail.MIME(); err == nil {
		t.Fatalf("expected mail open to fail after release")
	}
}

func TestEncryptingProcessorReturnsEncryptError(t *testing.T) {
	t.Parallel()

	processor := &EncryptingProcessor{
		Encryptor: &fakeMailEncryptor{err: errors.New("encrypt failed")},
		StaticPlan: mailflow.ExecutionPlan{
			Targets: []mailflow.DeliveryTarget{{
				Name:     "archive-main",
				Consumer: "archive",
				Artifact: "primary",
				Required: true,
			}},
		},
	}

	_, err := processor.Process(context.Background(), mailflow.MailEnvelope{
		MIME: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("source-mime")), nil
		},
		Trace: mailflow.MailTrace{TransactionKey: "tx-processor-err"},
	})
	if err == nil || !strings.Contains(err.Error(), "encrypt failed") {
		t.Fatalf("Process() error = %v, want encrypt failed", err)
	}
}

func TestEncryptingProcessorReturnsAlreadyEncryptedError(t *testing.T) {
	t.Parallel()

	processor := &EncryptingProcessor{
		Encryptor: &fakeMailEncryptor{err: encrypt.AlreadyEncryptedError{Format: "pgp-mime"}},
		StaticPlan: mailflow.ExecutionPlan{
			Targets: []mailflow.DeliveryTarget{{
				Name:     "archive-main",
				Consumer: "archive",
				Artifact: "primary",
				Required: true,
			}},
		},
	}

	_, err := processor.Process(context.Background(), mailflow.MailEnvelope{
		MIME: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("source-mime")), nil
		},
		Trace: mailflow.MailTrace{TransactionKey: "tx-processor-skip"},
	})
	if !errors.Is(err, encrypt.ErrAlreadyEncrypted) {
		t.Fatalf("Process() error = %v, want ErrAlreadyEncrypted", err)
	}
}

func TestEncryptingProcessorCleanupRemovesWorkDir(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	processor := &EncryptingProcessor{
		Encryptor: &fakeMailEncryptor{
			result: encrypt.Result{Encrypted: true, Format: "pgp-mime"},
		},
		WorkDir: workDir,
		StaticPlan: mailflow.ExecutionPlan{
			Targets: []mailflow.DeliveryTarget{{
				Name:     "archive-main",
				Consumer: "archive",
				Artifact: "primary",
				Required: true,
			}},
		},
	}

	result, err := processor.Process(context.Background(), mailflow.MailEnvelope{
		MIME: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("source-mime")), nil
		},
		Trace: mailflow.MailTrace{TransactionKey: "tx-processor-clean"},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	reader, err := result.Mail.MIME()
	if err != nil {
		t.Fatalf("mail MIME() error = %v", err)
	}
	file, ok := reader.(*os.File)
	if !ok {
		t.Fatalf("mail reader type = %T, want *os.File", reader)
	}
	path := file.Name()
	_ = reader.Close()

	if err := result.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected temp file to be removed, stat err = %v", err)
	}
}

func TestContextualProcessorRoutesAlreadyEncryptedMailToNoOp(t *testing.T) {
	t.Parallel()

	processor := &ContextualProcessor{
		Encrypting: &EncryptingProcessor{
			Encryptor: &fakeMailEncryptor{
				result: encrypt.Result{Encrypted: true, Format: "pgp-mime"},
			},
			StaticPlan: mailflow.ExecutionPlan{
				Targets: []mailflow.DeliveryTarget{{
					Name:     "archive-main",
					Consumer: "archive",
					Artifact: "primary",
					Required: true,
				}},
			},
		},
		NoOp: &NoOpProcessor{
			StaticPlan: mailflow.ExecutionPlan{
				Targets: []mailflow.DeliveryTarget{{
					Name:     "archive-main",
					Consumer: "archive",
					Artifact: "primary",
					Required: true,
				}},
			},
		},
	}

	result, err := processor.Process(context.Background(), mailflow.MailEnvelope{
		MIME: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(strings.Join([]string{
				"Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"",
				"",
				"body",
			}, "\r\n"))), nil
		},
		Trace: mailflow.MailTrace{TransactionKey: "tx-processor-noop"},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Trace.Attributes["already_encrypted"] != "true" {
		t.Fatalf("already_encrypted = %q, want true", result.Trace.Attributes["already_encrypted"])
	}
	if result.Trace.Attributes["format"] != "pgp-mime" {
		t.Fatalf("format = %q, want pgp-mime", result.Trace.Attributes["format"])
	}
	reader, err := result.Mail.MIME()
	if err != nil {
		t.Fatalf("mail MIME() error = %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	_ = reader.Close()
	if !strings.Contains(string(data), "multipart/encrypted") {
		t.Fatalf("mail data = %q, want passthrough encrypted MIME", string(data))
	}
}
