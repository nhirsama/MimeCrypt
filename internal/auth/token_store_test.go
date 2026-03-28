package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/99designs/keyring"
)

func TestTokenStoreLoadsFromKeyringPrimary(t *testing.T) {
	t.Parallel()

	ring := keyring.NewArrayKeyring(nil)
	store := &tokenStore{
		primary: &keyringTokenBackend{
			ring: ring,
			key:  "token:test",
		},
	}

	token := Token{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Scope:        "scope-a",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	if err := store.save(token); err != nil {
		t.Fatalf("save() error = %v", err)
	}

	loaded, err := store.load()
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if loaded.AccessToken != "access-1" {
		t.Fatalf("AccessToken = %q, want access-1", loaded.AccessToken)
	}
}

func TestTokenStoreMigratesFileTokenIntoKeyring(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	fileBackend := &fileTokenBackend{
		path:        filepath.Join(stateDir, "token.json"),
		legacyPaths: []string{filepath.Join(stateDir, "graph-token.json")},
	}
	if err := fileBackend.save(Token{
		AccessToken:  "legacy-access",
		RefreshToken: "legacy-refresh",
		TokenType:    "Bearer",
		Scope:        "scope-a",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("fileBackend.save() error = %v", err)
	}

	ring := keyring.NewArrayKeyring(nil)
	store := &tokenStore{
		primary: &keyringTokenBackend{
			ring: ring,
			key:  "token:test",
		},
		fallbacks: []tokenBackend{fileBackend},
		cleanup:   []tokenBackend{fileBackend},
	}

	loaded, err := store.load()
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if loaded.AccessToken != "legacy-access" {
		t.Fatalf("AccessToken = %q, want legacy-access", loaded.AccessToken)
	}

	if _, err := ring.Get("token:test"); err != nil {
		t.Fatalf("ring.Get() error = %v", err)
	}
	if _, err := os.Stat(fileBackend.path); !os.IsNotExist(err) {
		t.Fatalf("expected file token to be removed, stat err = %v", err)
	}
}
