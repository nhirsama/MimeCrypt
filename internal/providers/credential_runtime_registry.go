package providers

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers/graph"
)

type credentialRuntimeConfig struct {
	Name               string
	ApplyConfig        func(appconfig.Config, []string) appconfig.Config
	ConfigureLocal     func(appconfig.Config, appconfig.LocalConfig, []string, io.Reader, io.Writer) (appconfig.LocalConfig, error)
	BuildSession       func(appconfig.Config) (CredentialSession, error)
	BuildIdentityProbe func(appconfig.Config, []string, provider.TokenSource) (func(context.Context) (provider.User, error), error)
}

type credentialRevokeConfig struct {
	Name               string
	ApplyConfig        func(appconfig.Config, []string) appconfig.Config
	BuildRemoteRevoker func(appconfig.Config, []string, provider.TokenSource) (CredentialRemoteRevoker, error)
}

type credentialRuntimeRegistration struct {
	Name           string
	Aliases        []string
	DefaultKinds   []string
	SupportedKinds []string
	Login          *credentialRuntimeConfig
	Revoke         *credentialRevokeConfig
}

type credentialAuthHintProfile struct {
	Name       string
	Aliases    []string
	NeedsGraph bool
	NeedsEWS   bool
	NeedsIMAP  bool
}

const (
	oauthDeviceRuntimeName      = "oauth-device"
	legacyMicrosoftOAuthRuntime = "microsoft-oauth"
)

var credentialRuntimeRegistry = []credentialRuntimeRegistration{
	{
		Name:           oauthDeviceRuntimeName,
		Aliases:        []string{legacyMicrosoftOAuthRuntime},
		DefaultKinds:   []string{appconfig.CredentialKindOAuth, appconfig.CredentialKindSharedSession},
		SupportedKinds: []string{appconfig.CredentialKindOAuth, appconfig.CredentialKindSharedSession},
		Login: &credentialRuntimeConfig{
			Name: oauthDeviceRuntimeName,
			ApplyConfig: func(cfg appconfig.Config, hints []string) appconfig.Config {
				cfg.Auth = applyCredentialAuthHintProfiles(cfg.Auth, hints)
				return cfg
			},
			ConfigureLocal: configureOAuthDeviceLocalConfig,
			BuildSession: func(cfg appconfig.Config) (CredentialSession, error) {
				return auth.NewSession(cfg.Auth, nil)
			},
			BuildIdentityProbe: buildOAuthDeviceIdentityProbe,
		},
		Revoke: &credentialRevokeConfig{
			Name: oauthDeviceRuntimeName,
			ApplyConfig: func(cfg appconfig.Config, hints []string) appconfig.Config {
				cfg.Auth = applyCredentialAuthHintProfiles(cfg.Auth, hints)
				return cfg
			},
			BuildRemoteRevoker: func(cfg appconfig.Config, _ []string, tokenSource provider.TokenSource) (CredentialRemoteRevoker, error) {
				return graph.NewIdentityRevoker(cfg, tokenSource, nil)
			},
		},
	},
}

var credentialAuthHintRegistry = []credentialAuthHintProfile{
	{Name: "ews", NeedsGraph: true, NeedsEWS: true},
	{Name: "graph", NeedsGraph: true},
	{Name: "imap", NeedsIMAP: true},
}

func availableCredentialAuthHintNames() []string {
	names := make([]string, 0, len(credentialAuthHintRegistry))
	for _, profile := range credentialAuthHintRegistry {
		names = append(names, profile.Name)
	}
	sort.Strings(names)
	return names
}

// CredentialAuthHintsForDrivers 根据设备驱动声明的认证需求推导 credential auth hints。
// 这里显式使用 driver capabilities，而不是把 driver 名直接当作 auth hint。
func CredentialAuthHintsForDrivers(drivers ...string) []string {
	requirement := provider.AuthRequirement{}
	for _, driver := range drivers {
		info, ok := LookupDriverInfo(driver)
		if !ok {
			continue
		}
		requirement.Graph = requirement.Graph || info.Auth.Graph
		requirement.EWS = requirement.EWS || info.Auth.EWS
		requirement.IMAP = requirement.IMAP || info.Auth.IMAP
	}
	return credentialAuthHintsForRequirement(requirement)
}

func uniqueNormalizedCredentialAuthHints(hints []string) []string {
	seen := make(map[string]struct{}, len(hints))
	normalized := make([]string, 0, len(hints))
	for _, hint := range hints {
		hint = normalizeCredentialAuthHintName(hint)
		if hint == "" {
			continue
		}
		if _, ok := seen[hint]; ok {
			continue
		}
		seen[hint] = struct{}{}
		normalized = append(normalized, hint)
	}
	sort.Strings(normalized)
	return normalized
}

func containsCredentialAuthHint(hints []string, target string) bool {
	target = normalizeCredentialAuthHintName(target)
	for _, hint := range hints {
		if normalizeCredentialAuthHintName(hint) == target {
			return true
		}
	}
	return false
}

func filterCredentialAuthHintsByCandidateSet(hints, candidates []string) []string {
	if len(hints) == 0 || len(candidates) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		allowed[normalizeCredentialAuthHintName(candidate)] = struct{}{}
	}

	filtered := make([]string, 0, len(hints))
	for _, hint := range uniqueNormalizedCredentialAuthHints(hints) {
		if _, ok := allowed[hint]; !ok {
			continue
		}
		filtered = append(filtered, hint)
	}
	return filtered
}

func applyCredentialAuthHintProfiles(authCfg appconfig.AuthConfig, hints []string) appconfig.AuthConfig {
	hints = uniqueNormalizedCredentialAuthHints(hints)
	if len(hints) == 0 {
		return authCfg
	}

	needsGraph := false
	needsEWS := false
	needsIMAP := false
	for _, hint := range hints {
		profile, ok := lookupCredentialAuthHintProfile(hint)
		if !ok {
			continue
		}
		needsGraph = needsGraph || profile.NeedsGraph
		needsEWS = needsEWS || profile.NeedsEWS
		needsIMAP = needsIMAP || profile.NeedsIMAP
	}

	if !needsGraph {
		authCfg.GraphScopes = nil
	}
	if !needsEWS {
		authCfg.EWSScopes = nil
	}
	if !needsIMAP {
		authCfg.IMAPScopes = nil
	}
	return authCfg
}

func credentialAuthHintsForRequirement(requirement provider.AuthRequirement) []string {
	hints := make([]string, 0, 3)
	if requirement.Graph {
		hints = append(hints, "graph")
	}
	if requirement.EWS {
		hints = append(hints, "ews")
	}
	if requirement.IMAP {
		hints = append(hints, "imap")
	}
	return hints
}

func configuredCredentialRuntimeName(kind, runtimeName string) (string, error) {
	runtimeName = normalizeCredentialRuntimeName(runtimeName)
	if runtimeName == "" {
		return "", fmt.Errorf("credential 未配置运行时驱动，请先执行 login 进行交互式配置")
	}
	return validateCredentialRuntimeName(kind, runtimeName)
}

func effectiveCredentialRuntimeName(kind, runtimeName string) (string, error) {
	runtimeName = normalizeCredentialRuntimeName(runtimeName)
	if runtimeName == "" {
		runtimeName = defaultCredentialRuntimeName(kind)
		if runtimeName == "" {
			return "", fmt.Errorf("credential kind 不支持: %s", normalizedCredentialKind(kind))
		}
	}
	return validateCredentialRuntimeName(kind, runtimeName)
}

func resolveCredentialRuntimeConfig(kind, runtimeName string) (*credentialRuntimeConfig, string, error) {
	runtimeName, err := configuredCredentialRuntimeName(kind, runtimeName)
	if err != nil {
		return nil, "", err
	}
	registration, ok := lookupCredentialRuntimeRegistration(runtimeName)
	if !ok || registration.Login == nil {
		return nil, "", fmt.Errorf("credential runtime %s 不支持 kind: %s", runtimeName, normalizedCredentialKind(kind))
	}
	return registration.Login, registration.Name, nil
}

func resolveCredentialRuntimeConfigForConfig(kind, runtimeName string) (*credentialRuntimeConfig, string, error) {
	runtimeName, err := effectiveCredentialRuntimeName(kind, runtimeName)
	if err != nil {
		return nil, "", err
	}
	registration, ok := lookupCredentialRuntimeRegistration(runtimeName)
	if !ok || registration.Login == nil {
		return nil, "", fmt.Errorf("credential runtime %s 不支持 kind: %s", runtimeName, normalizedCredentialKind(kind))
	}
	return registration.Login, registration.Name, nil
}

func resolveCredentialRevokeConfig(kind, runtimeName string) (*credentialRevokeConfig, string, error) {
	runtimeName, err := configuredCredentialRuntimeName(kind, runtimeName)
	if err != nil {
		return nil, "", err
	}
	registration, ok := lookupCredentialRuntimeRegistration(runtimeName)
	if !ok || registration.Revoke == nil {
		return nil, "", fmt.Errorf("credential runtime %s 不支持 kind: %s", runtimeName, normalizedCredentialKind(kind))
	}
	return registration.Revoke, registration.Name, nil
}

func defaultCredentialRuntimeName(kind string) string {
	kind = normalizedCredentialKind(kind)
	for _, registration := range credentialRuntimeRegistry {
		for _, candidate := range registration.DefaultKinds {
			if normalizedCredentialKind(candidate) == kind {
				return registration.Name
			}
		}
	}
	return ""
}

func validateCredentialRuntimeName(kind, runtimeName string) (string, error) {
	kind = normalizedCredentialKind(kind)
	if _, ok := appconfig.LookupCredentialKindSpec(kind); !ok {
		return "", fmt.Errorf("credential kind 不支持: %s", kind)
	}

	registration, ok := lookupCredentialRuntimeRegistration(runtimeName)
	if !ok {
		return "", fmt.Errorf("credential runtime 不支持: %s", runtimeName)
	}
	for _, supportedKind := range registration.SupportedKinds {
		if normalizedCredentialKind(supportedKind) == kind {
			return registration.Name, nil
		}
	}
	return "", fmt.Errorf("credential runtime %s 不支持 kind: %s", registration.Name, kind)
}

func lookupCredentialRuntimeRegistration(runtimeName string) (credentialRuntimeRegistration, bool) {
	normalized := strings.ToLower(strings.TrimSpace(runtimeName))
	for _, registration := range credentialRuntimeRegistry {
		if normalizeCredentialRuntimeName(registration.Name) == normalized {
			return registration, true
		}
		for _, alias := range registration.Aliases {
			if strings.ToLower(strings.TrimSpace(alias)) == normalized {
				return registration, true
			}
		}
	}
	return credentialRuntimeRegistration{}, false
}

func lookupCredentialAuthHintProfile(name string) (credentialAuthHintProfile, bool) {
	normalized := normalizeCredentialAuthHintName(name)
	for _, profile := range credentialAuthHintRegistry {
		if normalizeCredentialAuthHintName(profile.Name) == normalized {
			return profile, true
		}
		for _, alias := range profile.Aliases {
			if normalizeCredentialAuthHintName(alias) == normalized {
				return profile, true
			}
		}
	}
	return credentialAuthHintProfile{}, false
}

func normalizeCredentialRuntimeName(runtimeName string) string {
	return strings.ToLower(strings.TrimSpace(runtimeName))
}

func normalizeCredentialAuthHintName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizedCredentialKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return appconfig.CredentialKindOAuth
	}
	return appconfig.NormalizeCredentialKind(kind)
}
