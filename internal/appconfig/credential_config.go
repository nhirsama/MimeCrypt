package appconfig

import (
	"path/filepath"
	"strings"
)

const credentialStateDirName = "credentials"

func DefaultCredentialStateDir(baseStateDir, credentialName string) string {
	baseStateDir = strings.TrimSpace(baseStateDir)
	credentialName = strings.TrimSpace(credentialName)
	if baseStateDir == "" {
		return ""
	}
	if credentialName == "" || credentialName == defaultTopologyCredentialName {
		return baseStateDir
	}
	return filepath.Join(baseStateDir, credentialStateDirName, sanitizeFileComponent(credentialName))
}

func (c Config) WithCredential(name string, credential Credential) Config {
	cfg := c

	if stateDir := credential.ResolvedStateDir(cfg.Auth.StateDir, name); stateDir != "" {
		cfg.Auth.StateDir = stateDir
	}
	if value := strings.TrimSpace(credential.ClientID); value != "" {
		cfg.Auth.ClientID = value
	}
	if value := strings.TrimSpace(credential.Tenant); value != "" {
		cfg.Auth.Tenant = value
	}
	if value := strings.TrimSpace(credential.AuthorityBaseURL); value != "" {
		cfg.Auth.AuthorityBaseURL = value
	}
	if value := strings.TrimSpace(credential.TokenStore); value != "" {
		cfg.Auth.TokenStore = value
	}
	if value := strings.TrimSpace(credential.KeyringService); value != "" {
		cfg.Auth.KeyringService = value
	}
	if len(credential.GraphScopes) > 0 {
		cfg.Auth.GraphScopes = append([]string(nil), credential.GraphScopes...)
	}
	if len(credential.EWSScopes) > 0 {
		cfg.Auth.EWSScopes = append([]string(nil), credential.EWSScopes...)
	}
	if len(credential.IMAPScopes) > 0 {
		cfg.Auth.IMAPScopes = append([]string(nil), credential.IMAPScopes...)
	}
	if value := strings.TrimSpace(credential.IMAPUsername); value != "" {
		cfg.Mail.Client.IMAPUsername = value
	}
	cfg.Mail.Client.IMAPUsername = ResolveStoredIMAPUsernamePreferStored(cfg.Auth.StateDir, cfg.Mail.Client.IMAPUsername)
	return cfg
}

func (c Credential) ResolvedStateDir(baseStateDir, credentialName string) string {
	if value := strings.TrimSpace(c.StateDir); value != "" {
		return value
	}
	return DefaultCredentialStateDir(baseStateDir, credentialName)
}
