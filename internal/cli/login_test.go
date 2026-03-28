package cli

import (
	"testing"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

func TestApplyLoginIMAPUsernameArgUsesPositionalArgWhenNoHigherPrioritySource(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{}
	cmd := &cobra.Command{Use: "login"}
	cmd.Flags().String("imap-username", "", "")

	got := applyLoginIMAPUsernameArg(cfg, cmd, []string{"user@example.com"})
	if got.Mail.Client.IMAPUsername != "user@example.com" {
		t.Fatalf("IMAPUsername = %q, want user@example.com", got.Mail.Client.IMAPUsername)
	}
}

func TestApplyLoginIMAPUsernameArgPreservesEnvOverride(t *testing.T) {
	t.Setenv("MIMECRYPT_IMAP_USERNAME", "env@example.com")
	cfg := appconfig.Config{
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{IMAPUsername: "env@example.com"},
		},
	}
	cmd := &cobra.Command{Use: "login"}
	cmd.Flags().String("imap-username", "", "")

	got := applyLoginIMAPUsernameArg(cfg, cmd, []string{"arg@example.com"})
	if got.Mail.Client.IMAPUsername != "env@example.com" {
		t.Fatalf("IMAPUsername = %q, want env@example.com", got.Mail.Client.IMAPUsername)
	}
}

func TestApplyLoginIMAPUsernameArgPreservesFlagOverride(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{IMAPUsername: "flag@example.com"},
		},
	}
	cmd := &cobra.Command{Use: "login"}
	cmd.Flags().String("imap-username", "", "")
	if err := cmd.Flags().Set("imap-username", "flag@example.com"); err != nil {
		t.Fatalf("Flags().Set() error = %v", err)
	}

	got := applyLoginIMAPUsernameArg(cfg, cmd, []string{"arg@example.com"})
	if got.Mail.Client.IMAPUsername != "flag@example.com" {
		t.Fatalf("IMAPUsername = %q, want flag@example.com", got.Mail.Client.IMAPUsername)
	}
}
