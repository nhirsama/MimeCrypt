package auth

import (
	"bytes"
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
	"mimecrypt/internal/fileutil"
)

var errTokenNotFound = errors.New("token not found")
var ErrLoginRequired = errors.New("未找到登录状态")

type tokenBackend interface {
	load() (Token, error)
	save(Token) error
	delete() error
}

type tokenStore struct {
	backend  tokenBackend
	identity string
	lockPath string
}

type fileTokenBackend struct {
	path string
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
	fileBackend := &fileTokenBackend{path: cfg.TokenPath()}

	if cfg.TokenStoreMode() != "keyring" {
		return &tokenStore{
			backend:  fileBackend,
			identity: tokenStoreIdentity(cfg),
			lockPath: tokenStoreLockPath(cfg),
		}, nil
	}

	ring, err := openSystemKeyring(cfg)
	if err != nil {
		return nil, err
	}

	return &tokenStore{
		backend: &keyringTokenBackend{
			ring: ring,
			key:  keyringTokenKey(cfg),
		},
		identity: tokenStoreIdentity(cfg),
		lockPath: tokenStoreLockPath(cfg),
	}, nil
}

func tokenStoreIdentity(cfg appconfig.AuthConfig) string {
	switch cfg.TokenStoreMode() {
	case "keyring":
		return "keyring:" + keyringTokenKey(cfg)
	default:
		return "file:" + filepath.Clean(cfg.TokenPath())
	}
}

func tokenStoreLockPath(cfg appconfig.AuthConfig) string {
	return filepath.Join(cfg.StateDir, ".token-store.lock")
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
	if s == nil || s.backend == nil {
		return Token{}, fmt.Errorf("token 存储未初始化")
	}

	token, err := s.backend.load()
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, errTokenNotFound) {
		return Token{}, err
	}
	return Token{}, fmt.Errorf("%w，请先执行 login", ErrLoginRequired)
}

func (s *tokenStore) save(token Token) error {
	if s == nil || s.backend == nil {
		return fmt.Errorf("token 存储未初始化")
	}
	return s.backend.save(token)
}

func (s *tokenStore) delete() error {
	if s == nil || s.backend == nil {
		return fmt.Errorf("token 存储未初始化")
	}
	return s.backend.delete()
}

func (s *fileTokenBackend) load() (Token, error) {
	path := strings.TrimSpace(s.path)
	if path == "" {
		return Token{}, fmt.Errorf("token 路径不能为空")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Token{}, errTokenNotFound
		}
		return Token{}, fmt.Errorf("读取 token 缓存失败: %w", err)
	}

	var token Token
	if err := json.Unmarshal(content, &token); err != nil {
		return Token{}, fmt.Errorf("解析 token 缓存失败: %w", err)
	}
	return token, nil
}

func (s *fileTokenBackend) save(token Token) error {
	if s.path == "" {
		return fmt.Errorf("token 路径不能为空")
	}

	return writeJSONFile(s.path, token, 0o600)
}

func (s *fileTokenBackend) delete() error {
	path := strings.TrimSpace(s.path)
	if path == "" {
		return fmt.Errorf("token 路径不能为空")
	}

	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errTokenNotFound
		}
		return fmt.Errorf("删除 token 缓存失败: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("删除 token 缓存失败: %w", err)
	}
	return nil
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
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}
	content = append(content, '\n')
	if _, err := fileutil.WriteFileAtomic(path, mode, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("写入状态文件失败: %w", err)
	}
	return nil
}
