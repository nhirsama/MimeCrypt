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
	for _, target := range route.Targets {
		if strings.EqualFold(strings.TrimSpace(target.Artifact), defaultBackupArtifact) {
			return true
		}
	}
	return false
}

func appendDefaultBackupTarget(route appconfig.Route) appconfig.Route {
	if routeHasBackupTarget(route) {
		return route
	}
	route.Targets = append(route.Targets, appconfig.RouteTarget{
		Name:     defaultBackupTargetName,
		SinkRef:  defaultBackupSinkRef,
		Artifact: defaultBackupArtifact,
		Required: true,
	})
	return route
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
