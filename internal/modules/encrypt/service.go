package encrypt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	envPGPRecipients = "MIMECRYPT_PGP_RECIPIENTS"
	envGPGBinary     = "MIMECRYPT_GPG_BINARY"
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

	armored, err := s.encryptor().Encrypt(ctx, mimeBytes, recipients)
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

func (s *Service) recipients(mimeBytes []byte) ([]string, error) {
	if s != nil && s.RecipientResolver != nil {
		return s.RecipientResolver(mimeBytes)
	}
	return s.resolveRecipients(mimeBytes)
}

func (s *Service) encryptor() MIMEEncryptor {
	if s != nil && s.Encryptor != nil {
		return s.Encryptor
	}
	return gpgEncryptor{binary: s.gpgBinary(), trustModel: s.gpgTrustModel()}
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
