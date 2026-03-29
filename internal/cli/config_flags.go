package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

type topologyConfigFlags struct {
	topologyFile string
	sourceName   string
	routeName    string
}

func newTopologyConfigFlags(cfg appconfig.Config) topologyConfigFlags {
	return topologyConfigFlags{
		topologyFile: cfg.TopologyPath,
	}
}

func (f *topologyConfigFlags) addFlags(cmd *cobra.Command) {
	f.addSourceFlags(cmd)
	cmd.Flags().StringVar(&f.routeName, "route", f.routeName, "选择 topology 中的 route 名称")
}

func (f *topologyConfigFlags) addSourceFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.topologyFile, "topology-file", f.topologyFile, "命名 source / sink / route 配置文件路径（JSON）")
	cmd.Flags().StringVar(&f.sourceName, "source", f.sourceName, "选择 topology 中的 source 名称")
}

func (f topologyConfigFlags) apply(cfg appconfig.Config) appconfig.Config {
	cfg.TopologyPath = strings.TrimSpace(f.topologyFile)
	return cfg
}

type credentialConfigFlags struct {
	topologyFile   string
	credentialName string
}

func newCredentialConfigFlags(cfg appconfig.Config) credentialConfigFlags {
	return credentialConfigFlags{
		topologyFile: cfg.TopologyPath,
	}
}

func (f *credentialConfigFlags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.topologyFile, "topology-file", f.topologyFile, "命名 source / sink / route / credential 配置文件路径（JSON）")
	cmd.Flags().StringVar(&f.credentialName, "credential", f.credentialName, "选择 topology 中的 credential 名称")
}

func (f credentialConfigFlags) apply(cfg appconfig.Config) appconfig.Config {
	cfg.TopologyPath = strings.TrimSpace(f.topologyFile)
	return cfg
}

type pipelineConfigFlags struct {
	workDir        string
	protectSubject bool
	backupDir      string
	backupKeyID    string
	auditLogPath   string
	auditStdout    bool
}

func newPipelineConfigFlags(cfg appconfig.Config) pipelineConfigFlags {
	return pipelineConfigFlags{
		workDir:        cfg.Mail.Pipeline.WorkDir,
		protectSubject: cfg.Mail.Pipeline.ProtectSubject,
		backupDir:      cfg.Mail.Pipeline.BackupDir,
		backupKeyID:    cfg.Mail.Pipeline.BackupKeyID,
		auditLogPath:   cfg.Mail.Pipeline.AuditLogPath,
		auditStdout:    cfg.Mail.Pipeline.AuditStdout,
	}
}

func (f *pipelineConfigFlags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.workDir, "work-dir", f.workDir, "处理过程临时目录；为空时使用系统临时目录")
	cmd.Flags().BoolVar(&f.protectSubject, "protect-subject", f.protectSubject, "将外层邮件主题写为 \"...\"")
	cmd.Flags().StringVar(&f.backupDir, "backup-dir", f.backupDir, "源邮件加密备份目录；保存 gpg 加密后的文件")
	cmd.Flags().StringVar(&f.backupKeyID, "backup-key-id", f.backupKeyID, "备份加密使用的 catch-all GPG key id；设置后所有备份统一用该 key")
	cmd.Flags().StringVar(&f.auditLogPath, "audit-log-path", f.auditLogPath, "审计日志输出路径（JSONL）")
	cmd.Flags().BoolVar(&f.auditStdout, "audit-stdout", f.auditStdout, "将审计日志同步输出到 stdout，便于容器日志采集")
}

func (f pipelineConfigFlags) apply(cfg appconfig.Config, cmd *cobra.Command) appconfig.Config {
	cfg.Mail.Pipeline.WorkDir = f.workDir
	cfg.Mail.Pipeline.ProtectSubject = f.protectSubject
	cfg.Mail.Pipeline.BackupDir = f.backupDir
	cfg.Mail.Pipeline.BackupKeyID = f.backupKeyID
	cfg.Mail.Pipeline.AuditStdout = f.auditStdout
	if cmd != nil && cmd.Flags().Changed("audit-log-path") {
		cfg.Mail.Pipeline.AuditLogPath = f.auditLogPath
	}
	return cfg
}
