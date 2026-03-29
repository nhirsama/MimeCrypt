package appruntime

import (
	"fmt"
	"strings"

	"mimecrypt/internal/appconfig"
)

type CredentialPlan struct {
	Topology       appconfig.Topology
	Credential     appconfig.Credential
	CredentialName string
	Config         appconfig.Config
	Custom         bool
}

func ResolveCredentialPlan(cfg appconfig.Config, explicit string) (CredentialPlan, error) {
	topologyPath := strings.TrimSpace(cfg.TopologyPath)
	if topologyPath == "" {
		if value := strings.TrimSpace(explicit); value != "" && value != "default" {
			return CredentialPlan{}, fmt.Errorf("legacy 模式只支持 credential=default")
		}
		return CredentialPlan{
			CredentialName: "default",
			Config:         cfg,
		}, nil
	}

	topology, err := appconfig.LoadTopologyFile(topologyPath)
	if err != nil {
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
	plan.Custom = true
	return plan, nil
}
