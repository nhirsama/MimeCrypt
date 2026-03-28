package cli

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
)

type providerConfigFlags struct {
	clientID         string
	tenant           string
	stateDir         string
	authorityBaseURL string
	graphBaseURL     string
	ewsBaseURL       string
	imapAddr         string
	imapUsername     string
}

func newProviderConfigFlags(cfg appconfig.Config) providerConfigFlags {
	return providerConfigFlags{
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

func (f *providerConfigFlags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.clientID, "client-id", f.clientID, "Microsoft Entra 应用的 Client ID")
	cmd.Flags().StringVar(&f.tenant, "tenant", f.tenant, "租户标识，默认使用 organizations")
	cmd.Flags().StringVar(&f.stateDir, "state-dir", f.stateDir, "本地状态目录")
	cmd.Flags().StringVar(&f.authorityBaseURL, "authority-base-url", f.authorityBaseURL, "Microsoft Entra 认证基础地址")
	cmd.Flags().StringVar(&f.graphBaseURL, "graph-base-url", f.graphBaseURL, "Microsoft Graph 基础地址")
	cmd.Flags().StringVar(&f.ewsBaseURL, "ews-base-url", f.ewsBaseURL, "EWS 基础地址")
	cmd.Flags().StringVar(&f.imapAddr, "imap-addr", f.imapAddr, "IMAP 服务地址，例如 outlook.office365.com:993")
	cmd.Flags().StringVar(&f.imapUsername, "imap-username", f.imapUsername, "IMAP 登录用户名，一般为邮箱地址")
}

func (f providerConfigFlags) apply(cfg appconfig.Config, cmd *cobra.Command) appconfig.Config {
	return syncConfig(cfg, f.clientID, f.tenant, f.stateDir, f.authorityBaseURL, f.graphBaseURL, f.ewsBaseURL, f.imapAddr, resolveIMAPUsernameForCommand(f.stateDir, f.imapUsername, cmd))
}

func resolveIMAPUsernameForCommand(stateDir, fallback string, cmd *cobra.Command) string {
	if value := strings.TrimSpace(os.Getenv("MIMECRYPT_IMAP_USERNAME")); value != "" {
		return value
	}
	if cmd != nil && cmd.Flags().Changed("imap-username") {
		return strings.TrimSpace(fallback)
	}
	localCfg, err := appconfig.LoadLocalConfig(stateDir)
	if err == nil && strings.TrimSpace(localCfg.IMAPUsername) != "" {
		return localCfg.IMAPUsername
	}
	return strings.TrimSpace(fallback)
}

type processingConfigFlags struct {
	outputDir         string
	saveOutput        bool
	workDir           string
	protectSubject    bool
	backupDir         string
	backupKeyID       string
	auditLogPath      string
	auditStdout       bool
	writeBackProvider string
	writeBackFolder   string
}

func newProcessingConfigFlags(cfg appconfig.Config) processingConfigFlags {
	return processingConfigFlags{
		outputDir:         cfg.Mail.Pipeline.OutputDir,
		saveOutput:        cfg.Mail.Pipeline.SaveOutput,
		workDir:           cfg.Mail.Pipeline.WorkDir,
		protectSubject:    cfg.Mail.Pipeline.ProtectSubject,
		backupDir:         cfg.Mail.Pipeline.BackupDir,
		backupKeyID:       cfg.Mail.Pipeline.BackupKeyID,
		auditLogPath:      cfg.Mail.Pipeline.AuditLogPath,
		auditStdout:       cfg.Mail.Pipeline.AuditStdout,
		writeBackProvider: cfg.Mail.Pipeline.WriteBackProvider,
		writeBackFolder:   cfg.Mail.Pipeline.WriteBackFolder,
	}
}

func (f *processingConfigFlags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.outputDir, "output-dir", f.outputDir, "处理结果输出目录")
	cmd.Flags().BoolVar(&f.saveOutput, "save-output", f.saveOutput, "是否将加密后的 PGP/MIME 额外保存到本地 output-dir，默认关闭")
	cmd.Flags().StringVar(&f.workDir, "work-dir", f.workDir, "处理过程临时目录；为空时使用系统临时目录")
	cmd.Flags().BoolVar(&f.protectSubject, "protect-subject", f.protectSubject, "是否将外层邮件主题写为 \"...\"，以贴近 Thunderbird 的加密主题展示")
	cmd.Flags().StringVar(&f.backupDir, "backup-dir", f.backupDir, "源邮件加密备份目录；保存 gpg 直接加密后的文件")
	cmd.Flags().StringVar(&f.backupKeyID, "backup-key-id", f.backupKeyID, "备份加密使用的 catch-all GPG key id；设置后所有备份统一用该 key")
	cmd.Flags().StringVar(&f.auditLogPath, "audit-log-path", f.auditLogPath, "审计日志输出路径（JSONL）")
	cmd.Flags().BoolVar(&f.auditStdout, "audit-stdout", f.auditStdout, "同时将审计日志输出到 stdout（适合容器日志采集）")
	cmd.Flags().StringVar(&f.writeBackProvider, "write-back-provider", f.writeBackProvider, "回写后端；可选 graph、ews 或 imap")
	cmd.Flags().StringVar(&f.writeBackFolder, "write-back-folder", f.writeBackFolder, "回写目标文件夹标识；默认回写到原文件夹")
}

func (f processingConfigFlags) apply(cfg appconfig.Config, cmd *cobra.Command) appconfig.Config {
	cfg.Mail.Pipeline.OutputDir = f.outputDir
	cfg.Mail.Pipeline.SaveOutput = f.saveOutput
	cfg.Mail.Pipeline.WorkDir = f.workDir
	cfg.Mail.Pipeline.ProtectSubject = f.protectSubject
	cfg.Mail.Pipeline.BackupDir = f.backupDir
	cfg.Mail.Pipeline.BackupKeyID = f.backupKeyID
	cfg.Mail.Pipeline.AuditStdout = f.auditStdout
	cfg.Mail.Pipeline.WriteBackProvider = f.writeBackProvider
	cfg.Mail.Pipeline.WriteBackFolder = f.writeBackFolder
	if cmd.Flags().Changed("audit-log-path") {
		cfg.Mail.Pipeline.AuditLogPath = f.auditLogPath
	}
	return cfg
}

type syncConfigFlags struct {
	folder       string
	pollInterval time.Duration
	cycleTimeout time.Duration
}

func newSyncConfigFlags(cfg appconfig.Config) syncConfigFlags {
	return syncConfigFlags{
		folder:       cfg.Mail.Sync.Folder,
		pollInterval: cfg.Mail.Sync.PollInterval,
		cycleTimeout: cfg.Mail.Sync.CycleTimeout,
	}
}

func (f *syncConfigFlags) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.folder, "folder", f.folder, "要监听的邮件文件夹标识；Graph 用 folder id，IMAP 用 mailbox 名称")
	cmd.Flags().DurationVar(&f.pollInterval, "poll-interval", f.pollInterval, "轮询增量同步的时间间隔")
	cmd.Flags().DurationVar(&f.cycleTimeout, "cycle-timeout", f.cycleTimeout, "单次发现与处理周期的超时时间")
}

func (f syncConfigFlags) apply(cfg appconfig.Config) appconfig.Config {
	cfg.Mail.Sync.Folder = f.folder
	cfg.Mail.Sync.PollInterval = f.pollInterval
	cfg.Mail.Sync.CycleTimeout = f.cycleTimeout
	return cfg
}
