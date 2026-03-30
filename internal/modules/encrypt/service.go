package encrypt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	message "github.com/emersion/go-message"
)

const (
	envPGPRecipients = "MIMECRYPT_PGP_RECIPIENTS"
	envGPGBinary     = "MIMECRYPT_GPG_BINARY"
	envGPGHome       = "MIMECRYPT_GPG_HOME"
	envGPGTrustModel = "MIMECRYPT_GPG_TRUST_MODEL"
)

type Service struct {
	Encryptor         MIMEEncryptor
	EnvLookup         func(string) string
	RecipientResolver func(mimeBytes []byte) ([]string, error)
	ProtectSubject    bool
}

type Result struct {
	Armored          []byte
	MIME             []byte
	Encrypted        bool
	AlreadyEncrypted bool
	Format           string
}

var ErrNoRecipients = errors.New("未找到可用的加密收件人，请设置 MIMECRYPT_PGP_RECIPIENTS 或在邮件头中提供 To/Cc/Bcc")
var ErrAlreadyEncrypted = errors.New("邮件已加密，拒绝重复加密")

type AlreadyEncryptedError struct {
	Format string
}

func (e AlreadyEncryptedError) Error() string {
	if strings.TrimSpace(e.Format) == "" {
		return ErrAlreadyEncrypted.Error()
	}
	return fmt.Sprintf("%s: %s", ErrAlreadyEncrypted.Error(), e.Format)
}

func (e AlreadyEncryptedError) Is(target error) bool {
	return target == ErrAlreadyEncrypted
}

type MIMEEncryptor interface {
	Encrypt(ctx context.Context, mimeBytes []byte, recipients []string) ([]byte, error)
}

type streamingMIMEEncryptor interface {
	EncryptTo(ctx context.Context, mimeBytes []byte, recipients []string, out io.Writer) error
}

type readerMIMEEncryptor interface {
	EncryptReaderTo(ctx context.Context, src io.Reader, recipients []string, out io.Writer) error
}

type MIMEOpenFunc func() (io.ReadCloser, error)

// Run 对邮件内容进行加密；已加密邮件会直接返回错误，防止重复加密。
func (s *Service) Run(mimeBytes []byte) (Result, error) {
	return s.RunContext(context.Background(), mimeBytes)
}

// RunContext 对邮件内容进行加密，并响应上下文取消。
func (s *Service) RunContext(ctx context.Context, mimeBytes []byte) (Result, error) {
	format, encrypted := detectFormat(mimeBytes)
	if encrypted {
		return Result{}, AlreadyEncryptedError{Format: format}
	}

	recipients, err := s.recipients(mimeBytes)
	if err != nil {
		return Result{}, err
	}
	if len(recipients) == 0 {
		return Result{}, ErrNoRecipients
	}

	encryptor := s.encryptor()
	if streamer, ok := encryptor.(streamingMIMEEncryptor); ok {
		var armored bytes.Buffer
		var encryptedMIME bytes.Buffer

		mimeWriter, err := newPGPMIMEMessageWriter(mimeBytes, &encryptedMIME, s != nil && s.ProtectSubject)
		if err != nil {
			return Result{}, err
		}

		if err := streamer.EncryptTo(ctx, mimeBytes, recipients, io.MultiWriter(&armored, mimeWriter)); err != nil {
			return Result{}, err
		}
		if err := mimeWriter.Close(); err != nil {
			return Result{}, err
		}

		return Result{
			Armored:          armored.Bytes(),
			MIME:             encryptedMIME.Bytes(),
			Encrypted:        true,
			AlreadyEncrypted: false,
			Format:           "pgp-mime",
		}, nil
	}

	armored, err := encryptor.Encrypt(ctx, mimeBytes, recipients)
	if err != nil {
		return Result{}, err
	}

	encryptedMIME, err := buildPGPMIMEMessage(mimeBytes, armored, s != nil && s.ProtectSubject)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Armored:          armored,
		MIME:             encryptedMIME,
		Encrypted:        true,
		AlreadyEncrypted: false,
		Format:           "pgp-mime",
	}, nil
}

// RunFromOpenerContext 从可重复打开的 MIME 源执行加密，优先使用流式实现。
func (s *Service) RunFromOpenerContext(ctx context.Context, open MIMEOpenFunc, armoredOut, mimeOut io.Writer) (Result, error) {
	if open == nil {
		return Result{}, fmt.Errorf("MIME 打开器不能为空")
	}

	format, encrypted, err := detectFormatFromOpener(mimeOpenFunc(open))
	if err != nil {
		return Result{}, fmt.Errorf("检测 MIME 加密格式失败: %w", err)
	}
	if encrypted {
		return Result{}, AlreadyEncryptedError{Format: format}
	}

	header, err := readHeaderFromOpener(open)
	if err != nil {
		return Result{}, err
	}
	recipients, err := s.resolveRecipientsFromHeader(header)
	if err != nil {
		return Result{}, err
	}
	if len(recipients) == 0 {
		return Result{}, ErrNoRecipients
	}

	encryptor := s.encryptor()
	readerEncryptor, ok := encryptor.(readerMIMEEncryptor)
	if !ok {
		mimeBytes, readErr := readAllFromOpener(open)
		if readErr != nil {
			return Result{}, readErr
		}
		result, runErr := s.RunContext(ctx, mimeBytes)
		if runErr != nil {
			return Result{}, runErr
		}
		if armoredOut != nil {
			if _, err := armoredOut.Write(result.Armored); err != nil {
				return Result{}, err
			}
		}
		if mimeOut != nil {
			if _, err := mimeOut.Write(result.MIME); err != nil {
				return Result{}, err
			}
		}
		result.Armored = nil
		result.MIME = nil
		return result, nil
	}

	src, err := open()
	if err != nil {
		return Result{}, fmt.Errorf("打开 MIME 源失败: %w", err)
	}
	defer src.Close()

	combinedOut := io.Writer(io.Discard)
	if armoredOut != nil {
		combinedOut = armoredOut
	}

	var mimeWriter *pgpMIMEMessageWriter
	if mimeOut != nil {
		mimeWriter, err = newPGPMIMEMessageWriterFromHeader(header, mimeOut, s != nil && s.ProtectSubject)
		if err != nil {
			return Result{}, err
		}
		combinedOut = io.MultiWriter(combinedOut, mimeWriter)
	}

	if err := readerEncryptor.EncryptReaderTo(ctx, src, recipients, combinedOut); err != nil {
		return Result{}, err
	}
	if mimeWriter != nil {
		if err := mimeWriter.Close(); err != nil {
			return Result{}, err
		}
	}

	return Result{
		Encrypted:        true,
		AlreadyEncrypted: false,
		Format:           "pgp-mime",
	}, nil
}

func (s *Service) recipients(mimeBytes []byte) ([]string, error) {
	if s != nil && s.RecipientResolver != nil {
		return s.RecipientResolver(mimeBytes)
	}
	return s.resolveRecipients(mimeBytes)
}

func (s *Service) resolveRecipientsFromHeader(header message.Header) ([]string, error) {
	if s != nil && s.RecipientResolver != nil {
		return s.RecipientResolver(headerToBytes(header))
	}

	recipients := collectRecipientsFromEnv(s.getenv(envPGPRecipients))
	for _, key := range recipientHeaderKeys {
		for _, value := range headerValues(header, key) {
			recipients = append(recipients, parseAddressList(value)...)
		}
	}
	recipients = dedupeRecipients(recipients)
	if len(recipients) == 0 {
		return nil, ErrNoRecipients
	}
	return recipients, nil
}

func (s *Service) encryptor() MIMEEncryptor {
	if s != nil && s.Encryptor != nil {
		return s.Encryptor
	}
	return gpgEncryptor{
		binary:     s.gpgBinary(),
		gpgHome:    s.gpgHome(),
		trustModel: s.gpgTrustModel(),
	}
}

func (s *Service) getenv(key string) string {
	if s != nil && s.EnvLookup != nil {
		return s.EnvLookup(key)
	}
	return os.Getenv(key)
}

func (s *Service) gpgBinary() string {
	if value := strings.TrimSpace(s.getenv(envGPGBinary)); value != "" {
		return value
	}
	return "gpg"
}

func (s *Service) gpgTrustModel() string {
	if value := strings.TrimSpace(s.getenv(envGPGTrustModel)); value != "" {
		return value
	}
	return defaultGPGTrustModel()
}

func (s *Service) gpgHome() string {
	return strings.TrimSpace(s.getenv(envGPGHome))
}

func readHeaderFromOpener(open MIMEOpenFunc) (message.Header, error) {
	reader, err := open()
	if err != nil {
		return message.Header{}, fmt.Errorf("打开 MIME 源失败: %w", err)
	}
	defer reader.Close()

	entity, err := message.Read(reader)
	if err != nil {
		return message.Header{}, fmt.Errorf("解析原始 MIME 失败: %w", err)
	}
	return entity.Header, nil
}

func readAllFromOpener(open MIMEOpenFunc) ([]byte, error) {
	reader, err := open()
	if err != nil {
		return nil, fmt.Errorf("打开 MIME 源失败: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("读取 MIME 源失败: %w", err)
	}
	return data, nil
}
