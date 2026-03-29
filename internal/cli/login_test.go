package cli

import (
	"testing"

	"mimecrypt/internal/appconfig"
)

func TestApplyLoginIMAPUsernameArgUsesPositionalArgWhenNoHigherPrioritySource(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{}

	got := applyLoginIMAPUsernameArg(cfg, []string{"user@example.com"})
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

	got := applyLoginIMAPUsernameArg(cfg, []string{"arg@example.com"})
	if got.Mail.Client.IMAPUsername != "env@example.com" {
		t.Fatalf("IMAPUsername = %q, want env@example.com", got.Mail.Client.IMAPUsername)
	}
}

func TestApplyLoginIMAPUsernameArgOverridesStoredConfigWhenExplicit(t *testing.T) {
	t.Parallel()

	cfg := appconfig.Config{
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{IMAPUsername: "stored@example.com"},
		},
	}

	got := applyLoginIMAPUsernameArg(cfg, []string{"arg@example.com"})
	if got.Mail.Client.IMAPUsername != "arg@example.com" {
		t.Fatalf("IMAPUsername = %q, want arg@example.com", got.Mail.Client.IMAPUsername)
	}
}
