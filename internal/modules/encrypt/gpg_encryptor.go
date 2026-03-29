package encrypt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type gpgEncryptor struct {
	binary     string
	gpgHome    string
	trustModel string
}

func defaultGPGBinary() string {
	if value := strings.TrimSpace(os.Getenv(envGPGBinary)); value != "" {
		return value
	}
	return "gpg"
}

func defaultGPGTrustModel() string {
	if value := strings.TrimSpace(os.Getenv(envGPGTrustModel)); value != "" {
		return value
	}
	return "auto"
}

func (g gpgEncryptor) Encrypt(ctx context.Context, mimeBytes []byte, recipients []string) ([]byte, error) {
	var out bytes.Buffer
	if err := g.EncryptReaderTo(ctx, bytes.NewReader(mimeBytes), recipients, &out); err != nil {
		return nil, err
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("gpg 输出为空")
	}
	return out.Bytes(), nil
}

func (g gpgEncryptor) EncryptTo(ctx context.Context, mimeBytes []byte, recipients []string, out io.Writer) error {
	return g.EncryptReaderTo(ctx, bytes.NewReader(mimeBytes), recipients, out)
}

func (g gpgEncryptor) EncryptReaderTo(ctx context.Context, src io.Reader, recipients []string, out io.Writer) error {
	if len(recipients) == 0 {
		return ErrNoRecipients
	}
	if out == nil {
		return fmt.Errorf("gpg 输出目标不能为空")
	}
	if src == nil {
		return fmt.Errorf("gpg 输入源不能为空")
	}

	binary := strings.TrimSpace(g.binary)
	if binary == "" {
		binary = "gpg"
	}
	trustModel := strings.TrimSpace(g.trustModel)
	if trustModel == "" {
		trustModel = defaultGPGTrustModel()
	}
	if err := validateGPGTrustModel(trustModel); err != nil {
		return err
	}

	args := []string{
		"--batch",
		"--yes",
		"--armor",
		"--trust-model",
		trustModel,
		"--encrypt",
		"--output",
		"-",
	}
	for _, recipient := range recipients {
		if err := ValidateRecipientSpec(recipient); err != nil {
			return err
		}
		args = append(args, "--recipient", recipient)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = src
	cmd.Stdout = out
	if gpgHome := strings.TrimSpace(g.gpgHome); gpgHome != "" {
		cmd.Env = append(os.Environ(), "GNUPGHOME="+gpgHome)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return fmt.Errorf("执行 gpg 失败: %w", err)
		}
		return fmt.Errorf("执行 gpg 失败: %w: %s", err, msg)
	}

	return nil
}

func validateGPGTrustModel(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "always", "auto", "classic", "direct", "tofu", "tofu+pgp", "pgp":
		return nil
	default:
		return fmt.Errorf("不支持的 GPG trust model: %s", value)
	}
}
