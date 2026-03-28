package auth

import (
	"errors"
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

type stubTokenBackend struct {
	loadFunc   func() (Token, error)
	saveFunc   func(Token) error
	deleteFunc func() error
}

func (b stubTokenBackend) load() (Token, error) {
	if b.loadFunc == nil {
		return Token{}, errTokenNotFound
	}
	return b.loadFunc()
}

func (b stubTokenBackend) save(token Token) error {
	if b.saveFunc == nil {
		return nil
	}
	return b.saveFunc(token)
}

func (b stubTokenBackend) delete() error {
	if b.deleteFunc == nil {
		return nil
	}
	return b.deleteFunc()
}

func TestTokenStoreLoadsFallbackWhenPrimaryKeyringUnavailable(t *testing.T) {
	t.Parallel()

	want := Token{
		AccessToken:  "fallback-access",
		RefreshToken: "fallback-refresh",
		TokenType:    "Bearer",
		Scope:        "scope-a",
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	store := &tokenStore{
		primary: stubTokenBackend{
			loadFunc: func() (Token, error) {
				return Token{}, errors.New("keyring temporarily unavailable")
			},
		},
		fallbacks: []tokenBackend{
			stubTokenBackend{
				loadFunc: func() (Token, error) { return want, nil },
			},
		},
	}

	got, err := store.load()
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Fatalf("load() = %+v, want %+v", got, want)
	}
}

func TestTokenStoreSaveIgnoresCleanupFailureAfterPrimarySuccess(t *testing.T) {
	t.Parallel()

	store := &tokenStore{
		primary: stubTokenBackend{
			saveFunc: func(Token) error { return nil },
		},
		cleanup: []tokenBackend{
			stubTokenBackend{
				deleteFunc: func() error { return errors.New("permission denied") },
			},
		},
	}

	if err := store.save(Token{AccessToken: "ok"}); err != nil {
		t.Fatalf("save() error = %v", err)
	}
}

func TestTokenStoreDeleteRemovesPrimaryAndFallbacks(t *testing.T) {
	t.Parallel()

	var deleted []string
	store := &tokenStore{
		primary: stubTokenBackend{
			deleteFunc: func() error {
				deleted = append(deleted, "primary")
				return nil
			},
		},
		fallbacks: []tokenBackend{
			stubTokenBackend{
				deleteFunc: func() error {
					deleted = append(deleted, "fallback-1")
					return nil
				},
			},
			stubTokenBackend{
				deleteFunc: func() error {
					deleted = append(deleted, "fallback-2")
					return errTokenNotFound
				},
			},
		},
	}

	if err := store.delete(); err != nil {
		t.Fatalf("delete() error = %v", err)
	}
	if len(deleted) != 3 {
		t.Fatalf("delete() removed %v, want all backends", deleted)
	}
}
