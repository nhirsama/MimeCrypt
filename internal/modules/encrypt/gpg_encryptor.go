package encrypt

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type gpgEncryptor struct {
	binary     string
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
	return "always"
}

func (g gpgEncryptor) Encrypt(ctx context.Context, mimeBytes []byte, recipients []string) ([]byte, error) {
	if len(recipients) == 0 {
		return nil, ErrNoRecipients
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
		return nil, err
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
			return nil, err
		}
		args = append(args, "--recipient", recipient)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = bytes.NewReader(mimeBytes)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, fmt.Errorf("执行 gpg 失败: %w", err)
		}
		return nil, fmt.Errorf("执行 gpg 失败: %w: %s", err, msg)
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("gpg 输出为空")
	}

	return stdout.Bytes(), nil
}

func validateGPGTrustModel(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "always", "auto", "classic", "direct", "tofu", "tofu+pgp", "pgp":
		return nil
	default:
		return fmt.Errorf("不支持的 GPG trust model: %s", value)
	}
}
