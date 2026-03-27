package encrypt

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	envPGPRecipients = "MIMECRYPT_PGP_RECIPIENTS"
	envGPGBinary     = "MIMECRYPT_GPG_BINARY"
)

type Service struct {
	Encryptor MIMEEncryptor
	EnvLookup func(string) string
}

type Result struct {
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
	Encrypt(mimeBytes []byte, recipients []string) ([]byte, error)
}

// Run 对邮件内容进行加密；已加密邮件会直接返回错误，防止重复加密。
func (s *Service) Run(mimeBytes []byte) (Result, error) {
	format, encrypted := detectFormat(mimeBytes)
	if encrypted {
		return Result{}, AlreadyEncryptedError{Format: format}
	}

	recipients, err := s.resolveRecipients(mimeBytes)
	if err != nil {
		return Result{}, err
	}
	if len(recipients) == 0 {
		return Result{}, ErrNoRecipients
	}

	armored, err := s.encryptor().Encrypt(mimeBytes, recipients)
	if err != nil {
		return Result{}, err
	}

	encryptedMIME, err := buildPGPMIMEMessage(mimeBytes, armored)
	if err != nil {
		return Result{}, err
	}

	return Result{
		MIME:             encryptedMIME,
		Encrypted:        true,
		AlreadyEncrypted: false,
		Format:           "pgp-mime",
	}, nil
}

func (s *Service) encryptor() MIMEEncryptor {
	if s != nil && s.Encryptor != nil {
		return s.Encryptor
	}
	return gpgEncryptor{binary: defaultGPGBinary()}
}

func (s *Service) getenv(key string) string {
	if s != nil && s.EnvLookup != nil {
		return s.EnvLookup(key)
	}
	return os.Getenv(key)
}
