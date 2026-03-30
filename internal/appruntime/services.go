package appruntime

import (
	"fmt"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/modules/login"
	"mimecrypt/internal/modules/revoke"
	"mimecrypt/internal/modules/tokenstate"
	"mimecrypt/internal/providers"
)

func BuildLoginService(plan CredentialPlan) (*login.Service, error) {
	runtime, err := providers.BuildCredentialRuntime(
		plan.EffectiveCredentialName(),
		plan.EffectiveCredentialKind(),
		plan.LocalConfig.LoginConfig,
		plan.Config,
		plan.AuthDrivers...,
	)
	if err != nil {
		return nil, err
	}

	service := &login.Service{
		Session:    runtime.Session,
		Credential: runtime.Name,
		Kind:       runtime.Kind,
		Runtime:    runtime.RuntimeName,
		Drivers:    append([]string(nil), runtime.Drivers...),
		StateDir:   runtime.Config.Auth.StateDir,
	}
	service.IdentityProbe = runtime.IdentityProbe
	return service, nil
}

func BuildRevokeService(plan CredentialPlan, force bool) (*revoke.Service, error) {
	loginRuntime, err := providers.BuildCredentialRuntime(
		plan.EffectiveCredentialName(),
		plan.EffectiveCredentialKind(),
		plan.LocalConfig.LoginConfig,
		plan.Config,
		plan.AuthDrivers...,
	)
	if err != nil {
		return nil, err
	}
	cfg := loginRuntime.Config

	kind, kindSpec, err := effectiveRevokeCredentialKind(plan)
	if err != nil {
		return nil, err
	}

	service := &revoke.Service{
		Session: loginRuntime.Session,
		ClearLocal: func() error {
			return appconfig.ClearLocalConfig(cfg.Auth.StateDir)
		},
		Force:         force,
		RequireRemote: kindSpec.RequiresRemoteRevoke,
	}
	if !kindSpec.RequiresRemoteRevoke {
		return service, nil
	}

	remoteRevoker, _, err := buildRemoteRevoker(cfg, kind, plan.LocalConfig.LoginConfig, loginRuntime.Session, plan.AuthDrivers...)
	if err != nil {
		if !force {
			return nil, fmt.Errorf("初始化远端吊销器失败: %w", err)
		}
		service.RemotePrepareErr = fmt.Errorf("初始化远端吊销器失败: %w", err)
		return service, nil
	}
	service.RemoteRevoker = remoteRevoker
	return service, nil
}

func effectiveRevokeCredentialKind(plan CredentialPlan) (string, appconfig.CredentialKindSpec, error) {
	kind := strings.TrimSpace(plan.Credential.Kind)
	if kind == "" {
		kind = appconfig.CredentialKindOAuth
	}
	spec, ok := appconfig.LookupCredentialKindSpec(kind)
	if !ok {
		return "", appconfig.CredentialKindSpec{}, fmt.Errorf("credential kind 不支持: %s", kind)
	}
	return appconfig.NormalizeCredentialKind(kind), spec, nil
}

func buildRemoteRevoker(cfg appconfig.Config, credentialKind, runtimeName string, tokenSource providers.CredentialSession, drivers ...string) (revoke.RemoteRevoker, appconfig.Config, error) {
	kind := appconfig.NormalizeCredentialKind(credentialKind)
	spec, ok := appconfig.LookupCredentialKindSpec(kind)
	if !ok {
		return nil, appconfig.Config{}, fmt.Errorf("credential kind 不支持: %s", credentialKind)
	}
	if !spec.RequiresRemoteRevoke {
		return nil, cfg, nil
	}
	revoker, effectiveCfg, err := providers.BuildRemoteRevoker(kind, runtimeName, cfg, tokenSource, drivers...)
	if err != nil {
		return nil, appconfig.Config{}, err
	}
	return revoker, effectiveCfg, nil
}

func BuildTokenStateService(plan CredentialPlan) (*tokenstate.Service, error) {
	runtime, err := providers.BuildCredentialRuntime(
		plan.EffectiveCredentialName(),
		plan.EffectiveCredentialKind(),
		plan.LocalConfig.LoginConfig,
		plan.Config,
		plan.AuthDrivers...,
	)
	if err != nil {
		return nil, err
	}

	return &tokenstate.Service{
		Credential:     runtime.Name,
		CredentialKind: runtime.Kind,
		Runtime:        runtime.RuntimeName,
		Drivers:        append([]string(nil), runtime.Drivers...),
		Session:        runtime.Session,
		StateDir:       runtime.Config.Auth.StateDir,
		TokenStore:     runtime.Config.Auth.TokenStoreMode(),
		KeyringService: runtime.Config.Auth.KeyringServiceName(),
	}, nil
}
