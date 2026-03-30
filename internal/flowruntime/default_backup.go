package flowruntime

import (
	"strings"

	"mimecrypt/internal/appconfig"
)

const (
	defaultBackupSinkRef    = "__default_backup__"
	defaultBackupTargetName = "backup"
	defaultBackupDriver     = "backup"
	defaultBackupArtifact   = "backup"
)

func backupEnabled(cfg appconfig.Config) bool {
	return strings.TrimSpace(cfg.Mail.Pipeline.BackupDir) != ""
}

func routeHasBackupTarget(route appconfig.Route) bool {
	return targetsHaveBackupArtifact(route.Targets)
}

func targetsHaveBackupArtifact(targets []appconfig.RouteTarget) bool {
	for _, target := range targets {
		if strings.EqualFold(strings.TrimSpace(target.Artifact), defaultBackupArtifact) {
			return true
		}
	}
	return false
}

func appendDefaultBackupTarget(targets []appconfig.RouteTarget) []appconfig.RouteTarget {
	if targetsHaveBackupArtifact(targets) {
		return targets
	}
	return append(targets, appconfig.RouteTarget{
		Name:     defaultBackupTargetName,
		SinkRef:  defaultBackupSinkRef,
		Artifact: defaultBackupArtifact,
		Required: true,
	})
}

func defaultBackupSinkPlan(cfg appconfig.Config) SinkPlan {
	return SinkPlan{
		Sink: appconfig.Sink{
			Name:      defaultBackupSinkRef,
			Driver:    defaultBackupDriver,
			OutputDir: cfg.Mail.Pipeline.BackupDir,
		},
		Config: cfg,
	}
}
