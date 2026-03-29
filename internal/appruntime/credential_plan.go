package appruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mimecrypt/internal/appconfig"
)

type CredentialPlan struct {
	Topology       appconfig.Topology
	Credential     appconfig.Credential
	CredentialName string
	Config         appconfig.Config
}

func ResolveCredentialPlan(cfg appconfig.Config, explicit string) (CredentialPlan, error) {
	topologyPath := strings.TrimSpace(cfg.TopologyPath)
	if topologyPath == "" {
		if strings.TrimSpace(explicit) != "" {
			return CredentialPlan{}, fmt.Errorf("credential %s 需要 topology 配置", strings.TrimSpace(explicit))
		}
		return resolveBootstrapCredentialPlan(cfg), nil
	}

	topology, err := appconfig.LoadTopologyFile(topologyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && isDefaultCredentialTopologyPath(cfg, topologyPath) {
			if strings.TrimSpace(explicit) != "" {
				return CredentialPlan{}, fmt.Errorf("credential %s 需要 topology 配置", strings.TrimSpace(explicit))
			}
			return resolveBootstrapCredentialPlan(cfg), nil
		}
		return CredentialPlan{}, err
	}
	credentialName, err := topology.ResolveCredentialRef(explicit)
	if err != nil {
		return CredentialPlan{}, err
	}

	plan := CredentialPlan{
		Topology: topology,
		Config:   cfg,
	}
	if credentialName == "" {
		return plan, nil
	}

	credential, err := topology.CredentialConfig(credentialName)
	if err != nil {
		return CredentialPlan{}, err
	}
	plan.Credential = credential
	plan.CredentialName = credential.Name
	plan.Config = cfg.WithCredential(credential.Name, credential)
	return plan, nil
}

func resolveBootstrapCredentialPlan(cfg appconfig.Config) CredentialPlan {
	return CredentialPlan{
		Config: cfg,
	}
}

func isDefaultCredentialTopologyPath(cfg appconfig.Config, topologyPath string) bool {
	defaultPath := appconfig.DefaultTopologyPath(cfg.Auth.StateDir)
	if strings.TrimSpace(defaultPath) == "" {
		return false
	}
	return filepath.Clean(strings.TrimSpace(topologyPath)) == filepath.Clean(defaultPath)
}
