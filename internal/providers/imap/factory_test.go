package imap

import (
	"testing"

	"mimecrypt/internal/appconfig"
)

func TestBuildSourceClientsWithSessionUsesExplicitFolder(t *testing.T) {
	t.Parallel()

	clients, err := BuildSourceClientsWithSession(testIMAPFactoryConfig(), "Archive/Sub", fakeTokenSource{})
	if err != nil {
		t.Fatalf("BuildSourceClientsWithSession() error = %v", err)
	}

	reader, ok := clients.Reader.(*reader)
	if !ok {
		t.Fatalf("Reader type = %T, want *reader", clients.Reader)
	}
	if got, want := reader.client.defaultFolder, "Archive/Sub"; got != want {
		t.Fatalf("defaultFolder = %q, want %q", got, want)
	}
}

func TestNewWriterClientsUsesExplicitFolder(t *testing.T) {
	t.Parallel()

	clients, err := NewWriterClients(testIMAPFactoryConfig(), "Encrypted", fakeTokenSource{})
	if err != nil {
		t.Fatalf("NewWriterClients() error = %v", err)
	}

	writer, ok := clients.Writer.(*writer)
	if !ok {
		t.Fatalf("Writer type = %T, want *writer", clients.Writer)
	}
	if got, want := writer.client.defaultFolder, "Encrypted"; got != want {
		t.Fatalf("defaultFolder = %q, want %q", got, want)
	}
}

func testIMAPFactoryConfig() appconfig.Config {
	return appconfig.Config{
		Auth: appconfig.AuthConfig{
			IMAPScopes: []string{"imap.read"},
		},
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{
				IMAPAddr:     "imap.example.com:993",
				IMAPUsername: "user@example.com",
			},
		},
	}
}
