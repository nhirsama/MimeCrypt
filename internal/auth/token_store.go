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
	loadRecord() (sessionRecord, error)
	saveRecord(sessionRecord) error
	delete() error
}

type tokenStore struct {
	backend  tokenBackend
	identity string
	lockPath string
}

type sessionRecord struct {
	Format string `json:"format,omitempty"`
	Cache  []byte `json:"cache,omitempty"`
	Token  Token  `json:"token,omitempty"`
}

const (
	sessionRecordFormatLegacy = "legacy-token"
	sessionRecordFormatMSAL   = "msal-v1"
)

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
	record, err := s.loadRecord()
	if err != nil {
		return Token{}, err
	}
	return record.Token, nil
}

func (s *tokenStore) save(token Token) error {
	record := sessionRecord{
		Format: sessionRecordFormatLegacy,
		Token:  token,
	}
	return s.saveRecord(record)
}

func (s *tokenStore) loadRecord() (sessionRecord, error) {
	if s == nil || s.backend == nil {
		return sessionRecord{}, fmt.Errorf("token 存储未初始化")
	}
	record, err := s.backend.loadRecord()
	if err == nil {
		if record.Format == "" {
			if len(record.Cache) > 0 {
				record.Format = sessionRecordFormatMSAL
			} else {
				record.Format = sessionRecordFormatLegacy
			}
		}
		return record, nil
	}
	if !errors.Is(err, errTokenNotFound) {
		return sessionRecord{}, err
	}
	return sessionRecord{}, fmt.Errorf("%w，请先执行 login", ErrLoginRequired)
}

func (s *tokenStore) loadRecordIfExists() (sessionRecord, bool, error) {
	if s == nil || s.backend == nil {
		return sessionRecord{}, false, fmt.Errorf("token 存储未初始化")
	}
	record, err := s.backend.loadRecord()
	if errors.Is(err, errTokenNotFound) {
		return sessionRecord{}, false, nil
	}
	if err != nil {
		return sessionRecord{}, false, err
	}
	if record.Format == "" {
		if len(record.Cache) > 0 {
			record.Format = sessionRecordFormatMSAL
		} else {
			record.Format = sessionRecordFormatLegacy
		}
	}
	return record, true, nil
}

func (s *tokenStore) saveRecord(record sessionRecord) error {
	if s == nil || s.backend == nil {
		return fmt.Errorf("token 存储未初始化")
	}
	return s.backend.saveRecord(record)
}

func (s *tokenStore) delete() error {
	if s == nil || s.backend == nil {
		return fmt.Errorf("token 存储未初始化")
	}
	return s.backend.delete()
}

func (s *fileTokenBackend) loadRecord() (sessionRecord, error) {
	path := strings.TrimSpace(s.path)
	if path == "" {
		return sessionRecord{}, fmt.Errorf("token 路径不能为空")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sessionRecord{}, errTokenNotFound
		}
		return sessionRecord{}, fmt.Errorf("读取 token 缓存失败: %w", err)
	}

	var record sessionRecord
	if err := json.Unmarshal(content, &record); err == nil && (record.Format != "" || len(record.Cache) > 0 || !tokenEmpty(record.Token)) {
		return record, nil
	}

	var token Token
	if err := json.Unmarshal(content, &token); err != nil {
		return sessionRecord{}, fmt.Errorf("解析 token 缓存失败: %w", err)
	}
	return sessionRecord{
		Format: sessionRecordFormatLegacy,
		Token:  token,
	}, nil
}

func (s *fileTokenBackend) saveRecord(record sessionRecord) error {
	if s.path == "" {
		return fmt.Errorf("token 路径不能为空")
	}

	return writeJSONFile(s.path, record, 0o600)
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

func (s *keyringTokenBackend) loadRecord() (sessionRecord, error) {
	if s == nil || s.ring == nil {
		return sessionRecord{}, fmt.Errorf("keyring 未初始化")
	}
	item, err := s.ring.Get(s.key)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return sessionRecord{}, errTokenNotFound
		}
		return sessionRecord{}, fmt.Errorf("读取系统 keyring 失败: %w", err)
	}

	var record sessionRecord
	if err := json.Unmarshal(item.Data, &record); err == nil && (record.Format != "" || len(record.Cache) > 0 || !tokenEmpty(record.Token)) {
		return record, nil
	}

	var token Token
	if err := json.Unmarshal(item.Data, &token); err != nil {
		return sessionRecord{}, fmt.Errorf("解析系统 keyring token 失败: %w", err)
	}
	return sessionRecord{
		Format: sessionRecordFormatLegacy,
		Token:  token,
	}, nil
}

func (s *keyringTokenBackend) saveRecord(record sessionRecord) error {
	if s == nil || s.ring == nil {
		return fmt.Errorf("keyring 未初始化")
	}

	content, err := json.Marshal(record)
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

func tokenEmpty(token Token) bool {
	return strings.TrimSpace(token.AccessToken) == "" &&
		strings.TrimSpace(token.RefreshToken) == "" &&
		strings.TrimSpace(token.TokenType) == "" &&
		strings.TrimSpace(token.Scope) == "" &&
		token.ExpiresAt.IsZero()
}
