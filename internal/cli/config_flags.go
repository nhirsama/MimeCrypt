package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

type baseConfigFlags struct {
	clientID         string
	tenant           string
	stateDir         string
	authorityBaseURL string
	graphBaseURL     string
	ewsBaseURL       string
	imapAddr         string
	imapUsername     string
}

func newBaseConfigFlags(cfg appconfig.Config) baseConfigFlags {
	return baseConfigFlags{
		clientID:         cfg.Auth.ClientID,
		tenant:           cfg.Auth.Tenant,
		stateDir:         cfg.Auth.StateDir,
		authorityBaseURL: cfg.Auth.AuthorityBaseURL,
		graphBaseURL:     cfg.Mail.Client.GraphBaseURL,
		ewsBaseURL:       cfg.Mail.Client.EWSBaseURL,
		imapAddr:         cfg.Mail.Client.IMAPAddr,
		imapUsername:     cfg.Mail.Client.IMAPUsername,
	}
}

func (f *baseConfigFlags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.clientID, "client-id", f.clientID, "Microsoft Entra 应用的 Client ID")
	cmd.Flags().StringVar(&f.tenant, "tenant", f.tenant, "租户标识；缺省值为 organizations")
	cmd.Flags().StringVar(&f.stateDir, "state-dir", f.stateDir, "本地状态目录")
	cmd.Flags().StringVar(&f.authorityBaseURL, "authority-base-url", f.authorityBaseURL, "Microsoft Entra 认证基础地址")
	cmd.Flags().StringVar(&f.graphBaseURL, "graph-base-url", f.graphBaseURL, "Microsoft Graph 基础地址")
	cmd.Flags().StringVar(&f.ewsBaseURL, "ews-base-url", f.ewsBaseURL, "EWS 基础地址")
	cmd.Flags().StringVar(&f.imapAddr, "imap-addr", f.imapAddr, "IMAP 服务地址，例如 outlook.office365.com:993")
	cmd.Flags().StringVar(&f.imapUsername, "imap-username", f.imapUsername, "IMAP 登录用户名，一般为邮箱地址")
}

func (f baseConfigFlags) apply(cfg appconfig.Config, cmd *cobra.Command) appconfig.Config {
	return applyBaseConfig(
		cfg,
		f.clientID,
		f.tenant,
		f.stateDir,
		f.authorityBaseURL,
		f.graphBaseURL,
		f.ewsBaseURL,
		f.imapAddr,
		resolveIMAPUsernameForCommand(f.stateDir, f.imapUsername, cmd),
	)
}

func resolveIMAPUsernameForCommand(stateDir, fallback string, cmd *cobra.Command) string {
	if value := strings.TrimSpace(os.Getenv("MIMECRYPT_IMAP_USERNAME")); value != "" {
		return value
	}
	if cmd != nil && cmd.Flags().Changed("imap-username") {
		return strings.TrimSpace(fallback)
	}
	return appconfig.ResolveStoredIMAPUsernamePreferStored(stateDir, fallback)
}

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
