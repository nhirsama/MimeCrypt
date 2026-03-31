package tokenstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
)

type Session interface {
	LoadCachedToken() (provider.Token, error)
	StoreToken(provider.Token) error
}

type Service struct {
	Credential     string
	CredentialKind string
	Runtime        string
	AuthProfile    string
	Session        Session
	StateDir       string
	TokenStore     string
	KeyringService string
}

type StatusResult struct {
	Credential     string
	CredentialKind string
	Runtime        string
	AuthProfile    string
	Present        bool
	StateDir       string
	TokenStore     string
	KeyringService string
	Token          provider.Token
}

type ImportResult struct {
	Credential     string
	CredentialKind string
	Runtime        string
	AuthProfile    string
	StateDir       string
	TokenStore     string
	KeyringService string
	Token          provider.Token
}

func (s *Service) Status() (StatusResult, error) {
	if s == nil || s.Session == nil {
		return StatusResult{}, fmt.Errorf("token session 不能为空")
	}

	token, err := s.Session.LoadCachedToken()
	if err != nil {
		if errors.Is(err, auth.ErrLoginRequired) {
			return StatusResult{
				Credential:     s.Credential,
				CredentialKind: s.CredentialKind,
				Runtime:        s.Runtime,
				AuthProfile:    s.AuthProfile,
				Present:        false,
				StateDir:       s.StateDir,
				TokenStore:     s.TokenStore,
				KeyringService: s.KeyringService,
			}, nil
		}
		return StatusResult{}, err
	}

	return StatusResult{
		Credential:     s.Credential,
		CredentialKind: s.CredentialKind,
		Runtime:        s.Runtime,
		AuthProfile:    s.AuthProfile,
		Present:        true,
		StateDir:       s.StateDir,
		TokenStore:     s.TokenStore,
		KeyringService: s.KeyringService,
		Token:          token,
	}, nil
}

func (s *Service) Import(src io.Reader) (ImportResult, error) {
	if s == nil || s.Session == nil {
		return ImportResult{}, fmt.Errorf("token session 不能为空")
	}
	if src == nil {
		return ImportResult{}, fmt.Errorf("token 输入源不能为空")
	}

	content, err := io.ReadAll(src)
	if err != nil {
		return ImportResult{}, fmt.Errorf("读取 token 输入失败: %w", err)
	}

	var token provider.Token
	if err := json.Unmarshal(content, &token); err != nil {
		return ImportResult{}, fmt.Errorf("解析 token JSON 失败: %w", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" && strings.TrimSpace(token.RefreshToken) == "" {
		return ImportResult{}, fmt.Errorf("token 缺少 access_token 和 refresh_token")
	}
	if token.ExpiresAt.IsZero() {
		token.ExpiresAt = time.Unix(0, 0).UTC()
	}

	if err := s.Session.StoreToken(token); err != nil {
		return ImportResult{}, err
	}

	return ImportResult{
		Credential:     s.Credential,
		CredentialKind: s.CredentialKind,
		Runtime:        s.Runtime,
		AuthProfile:    s.AuthProfile,
		StateDir:       s.StateDir,
		TokenStore:     s.TokenStore,
		KeyringService: s.KeyringService,
		Token:          token,
	}, nil
}
