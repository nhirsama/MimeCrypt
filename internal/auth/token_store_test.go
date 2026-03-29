package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/99designs/keyring"

	"mimecrypt/internal/appconfig"
)

func TestFileTokenBackendRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "token.json")
	backend := &fileTokenBackend{path: path}
	token := Token{
		AccessToken:  "access",
		RefreshToken: "refresh",
		Scope:        "scope-graph",
	}

	if err := backend.save(token); err != nil {
		t.Fatalf("save() error = %v", err)
	}

	loaded, err := backend.load()
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if loaded.AccessToken != "access" || loaded.RefreshToken != "refresh" {
		t.Fatalf("unexpected token: %+v", loaded)
	}

	if err := backend.delete(); err != nil {
		t.Fatalf("delete() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected token file to be removed, got %v", err)
	}
}

func TestTokenStoreLoadTranslatesMissingTokenToLoginRequired(t *testing.T) {
	t.Parallel()

	store := &tokenStore{
		backend: &fileTokenBackend{path: filepath.Join(t.TempDir(), "missing.json")},
	}

	_, err := store.load()
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("load() error = %v, want ErrLoginRequired", err)
	}
}

func TestNewTokenStoreBuildsFileBackendOnly(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store, err := newTokenStore(appconfig.AuthConfig{
		ClientID:   "client-id",
		Tenant:     "organizations",
		StateDir:   stateDir,
		TokenStore: "file",
		GraphScopes: []string{
			"scope-graph",
		},
	})
	if err != nil {
		t.Fatalf("newTokenStore() error = %v", err)
	}

	if store.backend == nil {
		t.Fatalf("backend = nil")
	}
	if store.identity != "file:"+filepath.Join(stateDir, "token.json") {
		t.Fatalf("identity = %q", store.identity)
	}
}

func TestKeyringTokenBackendRoundTrip(t *testing.T) {
	t.Parallel()

	ring := &fakeCredentialKeyring{items: map[string]keyring.Item{}}
	backend := &keyringTokenBackend{ring: ring, key: "token-key"}
	token := Token{AccessToken: "access", RefreshToken: "refresh"}

	if err := backend.save(token); err != nil {
		t.Fatalf("save() error = %v", err)
	}

	loaded, err := backend.load()
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if loaded.AccessToken != "access" {
		t.Fatalf("AccessToken = %q", loaded.AccessToken)
	}

	raw := ring.items["token-key"]
	var decoded Token
	if err := json.Unmarshal(raw.Data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded.RefreshToken != "refresh" {
		t.Fatalf("RefreshToken = %q", decoded.RefreshToken)
	}

	if err := backend.delete(); err != nil {
		t.Fatalf("delete() error = %v", err)
	}
}

type fakeCredentialKeyring struct {
	items map[string]keyring.Item
}

func (f *fakeCredentialKeyring) Get(key string) (keyring.Item, error) {
	item, ok := f.items[key]
	if !ok {
		return keyring.Item{}, keyring.ErrKeyNotFound
	}
	return item, nil
}

func (f *fakeCredentialKeyring) Set(item keyring.Item) error {
	f.items[item.Key] = item
	return nil
}

func (f *fakeCredentialKeyring) Remove(key string) error {
	if _, ok := f.items[key]; !ok {
		return keyring.ErrKeyNotFound
	}
	delete(f.items, key)
	return nil
}
