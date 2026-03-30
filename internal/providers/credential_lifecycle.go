package providers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/interact"
	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers/graph"
)

type CredentialSession interface {
	provider.TokenSource
	Login(context.Context, io.Writer) (provider.Token, error)
	Logout() error
	LoadCachedToken() (provider.Token, error)
	StoreToken(provider.Token) error
}

type CredentialRemoteRevoker interface {
	Revoke(context.Context, io.Writer) error
}

type CredentialRuntime struct {
	Name          string
	Kind          string
	Config        appconfig.Config
	Session       CredentialSession
	IdentityProbe func(context.Context) (provider.User, error)
	Drivers       []string
	RuntimeName   string
}

type LoginRuntime = CredentialRuntime

type credentialRuntimeConfig struct {
	Name               string
	ApplyConfig        func(appconfig.Config, []string) appconfig.Config
	ConfigureLocal     func(appconfig.Config, appconfig.LocalConfig, []string, io.Reader, io.Writer) (appconfig.LocalConfig, error)
	BuildSession       func(appconfig.Config) (CredentialSession, error)
	BuildIdentityProbe func(appconfig.Config, []string, provider.TokenSource) (func(context.Context) (provider.User, error), error)
}

type driverLoginConfig = credentialRuntimeConfig

type driverRevokeConfig struct {
	Name               string
	ApplyConfig        func(appconfig.Config, []string) appconfig.Config
	BuildRemoteRevoker func(appconfig.Config, []string, provider.TokenSource) (CredentialRemoteRevoker, error)
}

const (
	oauthDeviceRuntimeName      = "oauth-device"
	legacyMicrosoftOAuthRuntime = "microsoft-oauth"
)

var microsoftDriverLoginConfig = &credentialRuntimeConfig{
	Name: oauthDeviceRuntimeName,
	ApplyConfig: func(cfg appconfig.Config, drivers []string) appconfig.Config {
		cfg.Auth = applyOAuthDriverAuthConfig(cfg.Auth, drivers)
		return cfg
	},
	ConfigureLocal: configureOAuthDeviceLocalConfig,
	BuildSession: func(cfg appconfig.Config) (CredentialSession, error) {
		return auth.NewSession(cfg.Auth, nil)
	},
	BuildIdentityProbe: buildOAuthDeviceIdentityProbe,
}

var microsoftDriverRevokeConfig = &driverRevokeConfig{
	Name: oauthDeviceRuntimeName,
	ApplyConfig: func(cfg appconfig.Config, drivers []string) appconfig.Config {
		cfg.Auth = applyOAuthDriverAuthConfig(cfg.Auth, drivers)
		return cfg
	},
	BuildRemoteRevoker: func(cfg appconfig.Config, _ []string, tokenSource provider.TokenSource) (CredentialRemoteRevoker, error) {
		return graph.NewIdentityRevoker(cfg, tokenSource, nil)
	},
}

func BuildLoginRuntime(cfg appconfig.Config, drivers ...string) (LoginRuntime, error) {
	resolvedDrivers := effectiveCredentialDrivers(drivers...)
	loginConfig, runtimeName, err := resolveCredentialRuntimeConfigForConfig(appconfig.CredentialKindOAuth, "")
	if err != nil {
		return LoginRuntime{}, err
	}

	effectiveCfg := cfg
	if loginConfig.ApplyConfig != nil {
		effectiveCfg = loginConfig.ApplyConfig(cfg, resolvedDrivers)
	}

	session, err := loginConfig.BuildSession(effectiveCfg)
	if err != nil {
		return LoginRuntime{}, err
	}

	var identityProbe func(context.Context) (provider.User, error)
	if loginConfig.BuildIdentityProbe != nil {
		identityProbe, err = loginConfig.BuildIdentityProbe(effectiveCfg, resolvedDrivers, session)
		if err != nil {
			return LoginRuntime{}, err
		}
	}

	return LoginRuntime{
		Kind:          normalizedCredentialKind(appconfig.CredentialKindOAuth),
		Config:        effectiveCfg,
		Session:       session,
		IdentityProbe: identityProbe,
		Drivers:       resolvedDrivers,
		RuntimeName:   runtimeName,
	}, nil
}

func BuildCredentialRuntime(name, kind, runtimeName string, cfg appconfig.Config, drivers ...string) (CredentialRuntime, error) {
	resolvedDrivers := effectiveCredentialDrivers(drivers...)
	loginConfig, runtimeName, err := resolveCredentialRuntimeConfig(kind, runtimeName)
	if err != nil {
		return CredentialRuntime{}, err
	}

	effectiveCfg := cfg
	if loginConfig.ApplyConfig != nil {
		effectiveCfg = loginConfig.ApplyConfig(cfg, resolvedDrivers)
	}

	session, err := loginConfig.BuildSession(effectiveCfg)
	if err != nil {
		return CredentialRuntime{}, err
	}

	var identityProbe func(context.Context) (provider.User, error)
	if loginConfig.BuildIdentityProbe != nil {
		identityProbe, err = loginConfig.BuildIdentityProbe(effectiveCfg, resolvedDrivers, session)
		if err != nil {
			return CredentialRuntime{}, err
		}
	}

	return CredentialRuntime{
		Name:          strings.TrimSpace(name),
		Kind:          normalizedCredentialKind(kind),
		Config:        effectiveCfg,
		Session:       session,
		IdentityProbe: identityProbe,
		Drivers:       resolvedDrivers,
		RuntimeName:   runtimeName,
	}, nil
}

func ConfigureLoginLocalConfig(kind string, cfg appconfig.Config, localCfg appconfig.LocalConfig, in io.Reader, out io.Writer, drivers ...string) (appconfig.LocalConfig, appconfig.Config, []string, error) {
	if in == nil {
		in = strings.NewReader("")
	}
	if out == nil {
		out = io.Discard
	}

	reader, ok := in.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(in)
	}

	resolvedDrivers := effectiveCredentialDrivers(drivers...)
	if len(resolvedDrivers) == 0 {
		_, _ = fmt.Fprintln(out, "请选择 credential 需要支持的邮件驱动，用于补全最小 scopes 和协议参数，输入 q 可退出。")
		var err error
		resolvedDrivers, err = promptDriverSelection(reader, out, localCfg.Drivers)
		if err != nil {
			return appconfig.LocalConfig{}, appconfig.Config{}, nil, err
		}
	}
	loginConfig, runtimeName, err := resolveCredentialRuntimeConfigForConfig(kind, localCfg.LoginConfig)
	if err != nil {
		return appconfig.LocalConfig{}, appconfig.Config{}, nil, err
	}

	updated := localCfg
	updated.Drivers = append([]string(nil), resolvedDrivers...)
	updated.LoginConfig = runtimeName
	if loginConfig.ConfigureLocal != nil {
		updated, err = loginConfig.ConfigureLocal(cfg, updated, resolvedDrivers, reader, out)
		if err != nil {
			return appconfig.LocalConfig{}, appconfig.Config{}, nil, err
		}
	}
	effectiveCfg := cfg.WithLocalConfig(updated)
	if loginConfig.ApplyConfig != nil {
		effectiveCfg = loginConfig.ApplyConfig(effectiveCfg, resolvedDrivers)
	}
	return updated, effectiveCfg, resolvedDrivers, nil
}

func BuildRemoteRevoker(kind, runtimeName string, cfg appconfig.Config, tokenSource provider.TokenSource, drivers ...string) (CredentialRemoteRevoker, appconfig.Config, error) {
	resolvedDrivers := effectiveCredentialDrivers(drivers...)
	revokeConfig, _, err := resolveCredentialRevokeConfig(kind, runtimeName)
	if err != nil {
		return nil, appconfig.Config{}, err
	}

	effectiveCfg := cfg
	if revokeConfig.ApplyConfig != nil {
		effectiveCfg = revokeConfig.ApplyConfig(cfg, resolvedDrivers)
	}

	revoker, err := revokeConfig.BuildRemoteRevoker(effectiveCfg, resolvedDrivers, tokenSource)
	if err != nil {
		return nil, effectiveCfg, err
	}
	return revoker, effectiveCfg, nil
}

func buildOAuthDeviceIdentityProbe(cfg appconfig.Config, _ []string, tokenSource provider.TokenSource) (func(context.Context) (provider.User, error), error) {
	switch {
	case len(cfg.Auth.GraphScopes) > 0:
		clients, err := graph.BuildSourceClientsWithTokenSource(cfg, "", tokenSource)
		if err != nil {
			return nil, err
		}
		return clients.Reader.Me, nil
	case strings.TrimSpace(cfg.Mail.Client.IMAPUsername) != "":
		user := provider.User{
			Mail:              cfg.Mail.Client.IMAPUsername,
			UserPrincipalName: cfg.Mail.Client.IMAPUsername,
			DisplayName:       cfg.Mail.Client.IMAPUsername,
		}
		return func(context.Context) (provider.User, error) {
			return user, nil
		}, nil
	default:
		return nil, nil
	}
}

func configureOAuthDeviceLocalConfig(cfg appconfig.Config, localCfg appconfig.LocalConfig, drivers []string, in io.Reader, out io.Writer) (appconfig.LocalConfig, error) {
	if in == nil {
		in = strings.NewReader("")
	}
	if out == nil {
		out = io.Discard
	}

	reader, ok := in.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(in)
	}
	microsoft := localCfg.Microsoft
	if microsoft == nil {
		microsoft = &appconfig.MicrosoftLocalConfig{}
	}

	driverSummary := strings.Join(drivers, ",")
	if driverSummary == "" {
		driverSummary = "未指定"
	}
	_, _ = fmt.Fprintf(out, "配置 credential runtime: %s (drivers=%s，输入 q 可退出)\n", oauthDeviceRuntimeName, driverSummary)

	clientID, err := promptConfigValue(reader, out, "OAuth Client ID", cfg.Auth.ClientID, microsoft.ClientID)
	if err != nil {
		return appconfig.LocalConfig{}, err
	}
	tenant, err := promptConfigValue(reader, out, "OAuth Tenant", cfg.Auth.Tenant, microsoft.Tenant)
	if err != nil {
		return appconfig.LocalConfig{}, err
	}
	authorityBaseURL, err := promptConfigValue(reader, out, "OAuth Authority Base URL", cfg.Auth.AuthorityBaseURL, microsoft.AuthorityBaseURL)
	if err != nil {
		return appconfig.LocalConfig{}, err
	}

	microsoft.ClientID = clientID
	microsoft.Tenant = tenant
	microsoft.AuthorityBaseURL = authorityBaseURL
	hasDriverHints := len(uniqueNormalizedDrivers(drivers)) > 0
	if containsDriver(drivers, "imap") {
		imapUsername, err := promptConfigValue(reader, out, "IMAP Username", cfg.Mail.Client.IMAPUsername, microsoft.IMAPUsername)
		if err != nil {
			return appconfig.LocalConfig{}, err
		}
		microsoft.IMAPUsername = imapUsername
		localCfg.IMAPUsername = imapUsername
	} else if hasDriverHints {
		microsoft.IMAPUsername = ""
		localCfg.IMAPUsername = ""
	}

	localCfg.Microsoft = microsoft
	localCfg.LoginConfig = oauthDeviceRuntimeName
	localCfg.Drivers = append([]string(nil), drivers...)
	return localCfg, nil
}

func applyOAuthDriverAuthConfig(authCfg appconfig.AuthConfig, drivers []string) appconfig.AuthConfig {
	drivers = uniqueNormalizedDrivers(drivers)
	if len(drivers) == 0 {
		return authCfg
	}

	needsGraph := false
	needsEWS := false
	needsIMAP := false
	for _, driver := range drivers {
		switch driver {
		case "graph":
			needsGraph = true
		case "ews":
			needsGraph = true
			needsEWS = true
		case "imap":
			needsIMAP = true
		}
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

func effectiveCredentialDrivers(drivers ...string) []string {
	return uniqueNormalizedDrivers(drivers)
}

func uniqueNormalizedDrivers(drivers []string) []string {
	seen := make(map[string]struct{}, len(drivers))
	normalized := make([]string, 0, len(drivers))
	for _, driver := range drivers {
		driver = normalizeDriver(driver)
		if driver == "" {
			continue
		}
		if _, ok := seen[driver]; ok {
			continue
		}
		seen[driver] = struct{}{}
		normalized = append(normalized, driver)
	}
	sort.Strings(normalized)
	return normalized
}

func containsDriver(drivers []string, target string) bool {
	target = normalizeDriver(target)
	for _, driver := range drivers {
		if normalizeDriver(driver) == target {
			return true
		}
	}
	return false
}

func promptConfigValue(reader *bufio.Reader, out io.Writer, label, current, override string) (string, error) {
	current = strings.TrimSpace(current)
	override = strings.TrimSpace(override)

	effective := current
	if override != "" {
		effective = override
	}
	prompt := label
	if effective != "" {
		prompt += " [" + effective + "]"
	}
	prompt += " (回车保留，输入 - 清空覆盖): "
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return "", fmt.Errorf("输出交互提示失败: %w", err)
	}

	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("读取交互输入失败: %w", err)
	}
	value := strings.TrimSpace(line)
	if interact.IsAbortInput(value) {
		return "", interact.ErrAbort
	}
	switch value {
	case "":
		return effective, nil
	case "-":
		return "", nil
	default:
		return value, nil
	}
}

func promptDriverSelection(reader *bufio.Reader, out io.Writer, current []string) ([]string, error) {
	candidates := availableLoginDrivers()
	if len(candidates) == 0 {
		return nil, fmt.Errorf("当前没有支持交互式 login 配置的驱动")
	}

	current = filterDriversByCandidateSet(current, candidates)
	_, _ = fmt.Fprintln(out, "请选择 credential 需要支持的邮件驱动，可输入编号或名称，多个值用逗号分隔。")
	for idx, driver := range candidates {
		driverImpl, _ := LookupDriver(driver)
		configName := ""
		if loginDriver, ok := driverImpl.(loginConfigDriver); ok && loginDriver.LoginConfig() != nil {
			configName = loginDriver.LoginConfig().Name
		}
		if configName == "" {
			_, _ = fmt.Fprintf(out, "  %d) %s\n", idx+1, driver)
			continue
		}
		_, _ = fmt.Fprintf(out, "  %d) %s (%s)\n", idx+1, driver, configName)
	}

	for {
		prompt := "驱动"
		if len(current) > 0 {
			prompt += " [" + strings.Join(current, ",") + "]"
		}
		prompt += ": "
		if _, err := fmt.Fprint(out, prompt); err != nil {
			return nil, fmt.Errorf("输出驱动选择提示失败: %w", err)
		}

		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("读取驱动选择失败: %w", err)
		}
		selection := strings.TrimSpace(line)
		if interact.IsAbortInput(selection) {
			return nil, interact.ErrAbort
		}
		if selection == "" {
			if len(current) > 0 {
				return append([]string(nil), current...), nil
			}
			if err == io.EOF {
				return nil, fmt.Errorf("至少需要选择一个驱动")
			}
			_, _ = fmt.Fprintln(out, "至少需要选择一个驱动。")
			continue
		}

		selected, parseErr := parseDriverSelection(selection, candidates)
		if parseErr == nil {
			return selected, nil
		}
		if errors.Is(parseErr, interact.ErrAbort) {
			return nil, parseErr
		}
		if err == io.EOF {
			return nil, parseErr
		}
		_, _ = fmt.Fprintf(out, "驱动选择无效: %v\n", parseErr)
	}
}

func availableLoginDrivers() []string {
	drivers := make([]string, 0, len(registeredDrivers))
	for name, driverImpl := range registeredDrivers {
		if _, ok := driverImpl.(loginConfigDriver); !ok {
			continue
		}
		drivers = append(drivers, normalizeDriver(name))
	}
	sort.Strings(drivers)
	return drivers
}

func filterDriversByCandidateSet(drivers, candidates []string) []string {
	if len(drivers) == 0 || len(candidates) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		allowed[normalizeDriver(candidate)] = struct{}{}
	}

	filtered := make([]string, 0, len(drivers))
	for _, driver := range uniqueNormalizedDrivers(drivers) {
		if _, ok := allowed[driver]; !ok {
			continue
		}
		filtered = append(filtered, driver)
	}
	return filtered
}

func parseDriverSelection(selection string, candidates []string) ([]string, error) {
	tokens := strings.FieldsFunc(selection, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	if len(tokens) == 0 {
		return nil, fmt.Errorf("至少需要选择一个驱动")
	}

	candidateIndex := make(map[string]string, len(candidates))
	for idx, candidate := range candidates {
		candidateIndex[strconv.Itoa(idx+1)] = candidate
		candidateIndex[normalizeDriver(candidate)] = candidate
	}

	selected := make([]string, 0, len(tokens))
	for _, token := range tokens {
		key := normalizeDriver(token)
		driver, ok := candidateIndex[key]
		if !ok {
			return nil, fmt.Errorf("不支持的驱动: %s", strings.TrimSpace(token))
		}
		selected = append(selected, driver)
	}
	return uniqueNormalizedDrivers(selected), nil
}

func resolveCredentialRuntimeConfig(kind, runtimeName string) (*credentialRuntimeConfig, string, error) {
	runtimeName, err := configuredCredentialRuntimeName(kind, runtimeName)
	if err != nil {
		return nil, "", err
	}
	return lookupCredentialRuntimeConfig(kind, runtimeName)
}

func resolveCredentialRuntimeConfigForConfig(kind, runtimeName string) (*credentialRuntimeConfig, string, error) {
	runtimeName, err := effectiveCredentialRuntimeName(kind, runtimeName)
	if err != nil {
		return nil, "", err
	}
	return lookupCredentialRuntimeConfig(kind, runtimeName)
}

func lookupCredentialRuntimeConfig(kind, runtimeName string) (*credentialRuntimeConfig, string, error) {
	switch runtimeName {
	case oauthDeviceRuntimeName:
		return microsoftDriverLoginConfig, runtimeName, nil
	default:
		return nil, "", fmt.Errorf("credential runtime %s 不支持 kind: %s", runtimeName, normalizedCredentialKind(kind))
	}
}

func resolveCredentialRevokeConfig(kind, runtimeName string) (*driverRevokeConfig, string, error) {
	runtimeName, err := configuredCredentialRuntimeName(kind, runtimeName)
	if err != nil {
		return nil, "", err
	}

	switch runtimeName {
	case oauthDeviceRuntimeName:
		return microsoftDriverRevokeConfig, runtimeName, nil
	default:
		return nil, "", fmt.Errorf("credential runtime 不支持: %s", runtimeName)
	}
}

func resolveDriverRuntimeConfig(drivers []string) (*credentialRuntimeConfig, []string, error) {
	if len(drivers) == 0 {
		return nil, nil, nil
	}
	var selected *credentialRuntimeConfig
	for _, driver := range uniqueNormalizedDrivers(drivers) {
		driverImpl, ok := LookupDriver(driver)
		if !ok {
			return nil, nil, fmt.Errorf("不支持的 driver: %s", driver)
		}
		loginDriver, ok := driverImpl.(loginConfigDriver)
		if !ok || loginDriver.LoginConfig() == nil {
			continue
		}
		loginConfig := loginDriver.LoginConfig()
		if selected == nil {
			selected = loginConfig
			continue
		}
		if selected.Name != loginConfig.Name {
			return nil, nil, fmt.Errorf("driver %s 与 %s 需要不同的 login 配置，当前 credential 无法共享", drivers[0], driver)
		}
	}
	return selected, uniqueNormalizedDrivers(drivers), nil
}

func resolveDriverRevokeConfig(drivers []string) (*driverRevokeConfig, []string, error) {
	if len(drivers) == 0 {
		return nil, nil, nil
	}
	var selected *driverRevokeConfig
	for _, driver := range uniqueNormalizedDrivers(drivers) {
		driverImpl, ok := LookupDriver(driver)
		if !ok {
			return nil, nil, fmt.Errorf("不支持的 driver: %s", driver)
		}
		revokeDriver, ok := driverImpl.(revokeConfigDriver)
		if !ok || revokeDriver.RevokeConfig() == nil {
			continue
		}
		revokeConfig := revokeDriver.RevokeConfig()
		if selected == nil {
			selected = revokeConfig
			continue
		}
		if selected.Name != revokeConfig.Name {
			return nil, nil, fmt.Errorf("driver %s 与 %s 需要不同的 revoke 配置，当前 credential 无法共享", drivers[0], driver)
		}
	}
	return selected, uniqueNormalizedDrivers(drivers), nil
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

func validateCredentialRuntimeName(kind, runtimeName string) (string, error) {
	kind = normalizedCredentialKind(kind)
	if _, ok := appconfig.LookupCredentialKindSpec(kind); !ok {
		return "", fmt.Errorf("credential kind 不支持: %s", kind)
	}

	switch runtimeName {
	case oauthDeviceRuntimeName:
		switch kind {
		case appconfig.CredentialKindOAuth, appconfig.CredentialKindSharedSession:
			return runtimeName, nil
		}
	}
	return "", fmt.Errorf("credential runtime %s 不支持 kind: %s", runtimeName, kind)
}

func defaultCredentialRuntimeName(kind string) string {
	switch normalizedCredentialKind(kind) {
	case appconfig.CredentialKindOAuth, appconfig.CredentialKindSharedSession:
		return oauthDeviceRuntimeName
	default:
		return ""
	}
}

func normalizeCredentialRuntimeName(runtimeName string) string {
	switch strings.ToLower(strings.TrimSpace(runtimeName)) {
	case "", oauthDeviceRuntimeName:
		return strings.ToLower(strings.TrimSpace(runtimeName))
	case legacyMicrosoftOAuthRuntime:
		return oauthDeviceRuntimeName
	default:
		return strings.ToLower(strings.TrimSpace(runtimeName))
	}
}

func normalizedCredentialKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return appconfig.CredentialKindOAuth
	}
	return appconfig.NormalizeCredentialKind(kind)
}
