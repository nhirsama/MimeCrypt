package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/99designs/keyring"

	"mimecrypt/internal/appconfig"
)

var errTokenNotFound = errors.New("token not found")

type tokenBackend interface {
	load() (Token, error)
	save(Token) error
	delete() error
}

type tokenStore struct {
	primary   tokenBackend
	fallbacks []tokenBackend
	cleanup   []tokenBackend
}

type fileTokenBackend struct {
	path        string
	legacyPaths []string
}

type credentialKeyring interface {
	Get(key string) (keyring.Item, error)
	Set(item keyring.Item) error
	Remove(key string) error
}

type keyringTokenBackend struct {
	ring credentialKeyring
	key  string
}

func newTokenStore(cfg appconfig.AuthConfig) (*tokenStore, error) {
	fileBackend := &fileTokenBackend{
		path:        cfg.TokenPath(),
		legacyPaths: cfg.LegacyTokenPaths(),
	}

	if cfg.TokenStoreMode() != "keyring" {
		return &tokenStore{primary: fileBackend}, nil
	}

	ring, err := openSystemKeyring(cfg)
	if err != nil {
		return nil, err
	}

	return &tokenStore{
		primary: &keyringTokenBackend{
			ring: ring,
			key:  keyringTokenKey(cfg),
		},
		fallbacks: []tokenBackend{fileBackend},
		cleanup:   []tokenBackend{fileBackend},
	}, nil
}

func openSystemKeyring(cfg appconfig.AuthConfig) (credentialKeyring, error) {
	ring, err := keyring.Open(keyring.Config{
		ServiceName: cfg.KeyringServiceName(),
		AllowedBackends: []keyring.BackendType{
			keyring.WinCredBackend,
			keyring.KeychainBackend,
			keyring.SecretServiceBackend,
			keyring.KWalletBackend,
			keyring.KeyCtlBackend,
			keyring.PassBackend,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("打开系统 keyring 失败: %w", err)
	}
	return ring, nil
}

func keyringTokenKey(cfg appconfig.AuthConfig) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		cfg.StateDir,
		cfg.ClientID,
		cfg.Tenant,
		cfg.KeyringServiceName(),
	}, "\x00")))
	return "token:" + hex.EncodeToString(sum[:])
}

func (s *tokenStore) load() (Token, error) {
	if s == nil || s.primary == nil {
		return Token{}, fmt.Errorf("token 存储未初始化")
	}

	token, primaryErr := s.primary.load()
	if primaryErr == nil {
		return token, nil
	}

	var lastErr error
	for _, backend := range s.fallbacks {
		token, err := backend.load()
		if err == nil {
			if saveErr := s.primary.save(token); saveErr == nil {
				for _, cleanup := range s.cleanup {
					_ = cleanup.delete()
				}
			}
			return token, nil
		}
		if errors.Is(err, errTokenNotFound) {
			continue
		}
		lastErr = err
	}

	if lastErr != nil {
		return Token{}, lastErr
	}
	if primaryErr != nil && !errors.Is(primaryErr, errTokenNotFound) {
		return Token{}, primaryErr
	}
	return Token{}, fmt.Errorf("未找到登录状态，请先执行 login")
}

func (s *tokenStore) save(token Token) error {
	if s == nil || s.primary == nil {
		return fmt.Errorf("token 存储未初始化")
	}
	if err := s.primary.save(token); err != nil {
		return err
	}
	for _, backend := range s.cleanup {
		if err := backend.delete(); err != nil && !errors.Is(err, errTokenNotFound) {
			continue
		}
	}
	return nil
}

func (s *tokenStore) delete() error {
	if s == nil || s.primary == nil {
		return fmt.Errorf("token 存储未初始化")
	}

	backends := []tokenBackend{s.primary}
	backends = append(backends, s.fallbacks...)
	var lastErr error
	for _, backend := range backends {
		if err := backend.delete(); err != nil && !errors.Is(err, errTokenNotFound) {
			lastErr = err
		}
	}
	return lastErr
}

func (s *fileTokenBackend) load() (Token, error) {
	paths := s.paths()
	if len(paths) == 0 {
		return Token{}, fmt.Errorf("token 路径不能为空")
	}

	var lastErr error
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			lastErr = fmt.Errorf("读取 token 缓存失败: %w", err)
			continue
		}

		var token Token
		if err := json.Unmarshal(content, &token); err != nil {
			return Token{}, fmt.Errorf("解析 token 缓存失败: %w", err)
		}

		return token, nil
	}

	if lastErr != nil {
		return Token{}, lastErr
	}
	return Token{}, errTokenNotFound
}

func (s *fileTokenBackend) save(token Token) error {
	if s.path == "" {
		return fmt.Errorf("token 路径不能为空")
	}

	if err := writeJSONFile(s.path, token, 0o600); err != nil {
		return err
	}
	for _, path := range s.legacyPaths {
		if path == "" || path == s.path {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("删除旧 token 缓存失败: %w", err)
		}
	}
	return nil
}

func (s *fileTokenBackend) delete() error {
	paths := s.paths()
	if len(paths) == 0 {
		return fmt.Errorf("token 路径不能为空")
	}

	found := false
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			found = true
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("删除 token 缓存失败: %w", err)
		}
	}
	if !found {
		return errTokenNotFound
	}
	return nil
}

func (s *fileTokenBackend) paths() []string {
	if s == nil {
		return nil
	}

	seen := make(map[string]struct{})
	paths := make([]string, 0, 1+len(s.legacyPaths))
	for _, path := range append([]string{s.path}, s.legacyPaths...) {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func (s *keyringTokenBackend) load() (Token, error) {
	if s == nil || s.ring == nil {
		return Token{}, fmt.Errorf("keyring 未初始化")
	}
	item, err := s.ring.Get(s.key)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return Token{}, errTokenNotFound
		}
		return Token{}, fmt.Errorf("读取系统 keyring 失败: %w", err)
	}

	var token Token
	if err := json.Unmarshal(item.Data, &token); err != nil {
		return Token{}, fmt.Errorf("解析系统 keyring token 失败: %w", err)
	}
	return token, nil
}

func (s *keyringTokenBackend) save(token Token) error {
	if s == nil || s.ring == nil {
		return fmt.Errorf("keyring 未初始化")
	}

	content, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("序列化 token 失败: %w", err)
	}

	if err := s.ring.Set(keyring.Item{
		Key:         s.key,
		Data:        content,
		Label:       "MimeCrypt Token",
		Description: "MimeCrypt OAuth token cache",
	}); err != nil {
		return fmt.Errorf("写入系统 keyring 失败: %w", err)
	}
	return nil
}

func (s *keyringTokenBackend) delete() error {
	if s == nil || s.ring == nil {
		return fmt.Errorf("keyring 未初始化")
	}

	if err := s.ring.Remove(s.key); err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return errTokenNotFound
		}
		return fmt.Errorf("删除系统 keyring token 失败: %w", err)
	}
	return nil
}

func writeJSONFile(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建状态目录失败: %w", err)
	}

	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, mode); err != nil {
		return fmt.Errorf("写入临时状态文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("替换状态文件失败: %w", err)
	}

	return nil
}
