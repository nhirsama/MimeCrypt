package providers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
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

type LoginRuntime struct {
	Config        appconfig.Config
	Session       CredentialSession
	IdentityProbe func(context.Context) (provider.User, error)
	Drivers       []string
	ConfigName    string
}

type driverLoginConfig struct {
	Name               string
	ApplyConfig        func(appconfig.Config, []string) appconfig.Config
	ConfigureLocal     func(appconfig.Config, appconfig.LocalConfig, []string, io.Reader, io.Writer) (appconfig.LocalConfig, error)
	BuildSession       func(appconfig.Config) (CredentialSession, error)
	BuildIdentityProbe func(appconfig.Config, []string, provider.TokenSource) (func(context.Context) (provider.User, error), error)
}

type driverRevokeConfig struct {
	Name               string
	ApplyConfig        func(appconfig.Config, []string) appconfig.Config
	BuildRemoteRevoker func(appconfig.Config, []string, provider.TokenSource) (CredentialRemoteRevoker, error)
}

const microsoftCredentialConfigName = "microsoft-oauth"

var microsoftDriverLoginConfig = &driverLoginConfig{
	Name: microsoftCredentialConfigName,
	ApplyConfig: func(cfg appconfig.Config, drivers []string) appconfig.Config {
		cfg.Auth = applyMicrosoftDriverAuthConfig(cfg.Auth, drivers)
		return cfg
	},
	ConfigureLocal: configureMicrosoftLocalConfig,
	BuildSession: func(cfg appconfig.Config) (CredentialSession, error) {
		return auth.NewSession(cfg.Auth, nil)
	},
	BuildIdentityProbe: buildMicrosoftLoginIdentityProbe,
}

var microsoftDriverRevokeConfig = &driverRevokeConfig{
	Name: microsoftCredentialConfigName,
	ApplyConfig: func(cfg appconfig.Config, drivers []string) appconfig.Config {
		cfg.Auth = applyMicrosoftDriverAuthConfig(cfg.Auth, drivers)
		return cfg
	},
	BuildRemoteRevoker: func(cfg appconfig.Config, _ []string, tokenSource provider.TokenSource) (CredentialRemoteRevoker, error) {
		return graph.NewIdentityRevoker(cfg, tokenSource, nil)
	},
}

func BuildLoginRuntime(cfg appconfig.Config, drivers ...string) (LoginRuntime, error) {
	resolvedDrivers := effectiveCredentialDrivers(drivers...)
	loginConfig, resolvedDrivers, err := resolveDriverLoginConfig(resolvedDrivers)
	if err != nil {
		return LoginRuntime{}, err
	}
	if loginConfig == nil {
		return LoginRuntime{}, fmt.Errorf("credential 未配置登录驱动，请先执行 login 进行交互式配置")
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
		Config:        effectiveCfg,
		Session:       session,
		IdentityProbe: identityProbe,
		Drivers:       resolvedDrivers,
		ConfigName:    loginConfig.Name,
	}, nil
}

func ConfigureLoginLocalConfig(cfg appconfig.Config, localCfg appconfig.LocalConfig, in io.Reader, out io.Writer, drivers ...string) (appconfig.LocalConfig, appconfig.Config, []string, error) {
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
		var err error
		resolvedDrivers, err = promptDriverSelection(reader, out, localCfg.Drivers)
		if err != nil {
			return appconfig.LocalConfig{}, appconfig.Config{}, nil, err
		}
	}
	loginConfig, resolvedDrivers, err := resolveDriverLoginConfig(resolvedDrivers)
	if err != nil {
		return appconfig.LocalConfig{}, appconfig.Config{}, nil, err
	}
	if loginConfig == nil {
		return appconfig.LocalConfig{}, appconfig.Config{}, nil, fmt.Errorf("未找到可用的 login 驱动配置")
	}

	updated := localCfg
	updated.Drivers = append([]string(nil), resolvedDrivers...)
	updated.LoginConfig = loginConfig.Name
	if loginConfig.ConfigureLocal != nil {
		updated, err = loginConfig.ConfigureLocal(cfg, updated, resolvedDrivers, reader, out)
		if err != nil {
			return appconfig.LocalConfig{}, appconfig.Config{}, nil, err
		}
	}
	return updated, cfg.WithLocalConfig(updated), resolvedDrivers, nil
}

func BuildRemoteRevoker(cfg appconfig.Config, tokenSource provider.TokenSource, drivers ...string) (CredentialRemoteRevoker, appconfig.Config, error) {
	resolvedDrivers := effectiveCredentialDrivers(drivers...)
	revokeConfig, resolvedDrivers, err := resolveDriverRevokeConfig(resolvedDrivers)
	if err != nil {
		return nil, appconfig.Config{}, err
	}
	if revokeConfig == nil {
		return nil, cfg, fmt.Errorf("credential 未配置 revoke 驱动，请先执行 login 进行交互式配置")
	}

	effectiveCfg := cfg
	if revokeConfig.ApplyConfig != nil {
		effectiveCfg = revokeConfig.ApplyConfig(cfg, resolvedDrivers)
	}

	revoker, err := revokeConfig.BuildRemoteRevoker(effectiveCfg, resolvedDrivers, tokenSource)
	if err != nil {
		return nil, appconfig.Config{}, err
	}
	return revoker, effectiveCfg, nil
}

func buildMicrosoftLoginIdentityProbe(cfg appconfig.Config, _ []string, tokenSource provider.TokenSource) (func(context.Context) (provider.User, error), error) {
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

func configureMicrosoftLocalConfig(cfg appconfig.Config, localCfg appconfig.LocalConfig, drivers []string, in io.Reader, out io.Writer) (appconfig.LocalConfig, error) {
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

	_, _ = fmt.Fprintf(out, "配置登录驱动: microsoft-oauth (drivers=%s)\n", strings.Join(drivers, ","))

	clientID, err := promptConfigValue(reader, out, "Client ID", cfg.Auth.ClientID, microsoft.ClientID)
	if err != nil {
		return appconfig.LocalConfig{}, err
	}
	tenant, err := promptConfigValue(reader, out, "Tenant", cfg.Auth.Tenant, microsoft.Tenant)
	if err != nil {
		return appconfig.LocalConfig{}, err
	}
	authorityBaseURL, err := promptConfigValue(reader, out, "Authority Base URL", cfg.Auth.AuthorityBaseURL, microsoft.AuthorityBaseURL)
	if err != nil {
		return appconfig.LocalConfig{}, err
	}

	microsoft.ClientID = clientID
	microsoft.Tenant = tenant
	microsoft.AuthorityBaseURL = authorityBaseURL
	if containsDriver(drivers, "imap") {
		imapUsername, err := promptConfigValue(reader, out, "IMAP Username", cfg.Mail.Client.IMAPUsername, microsoft.IMAPUsername)
		if err != nil {
			return appconfig.LocalConfig{}, err
		}
		microsoft.IMAPUsername = imapUsername
		localCfg.IMAPUsername = imapUsername
	} else {
		microsoft.IMAPUsername = ""
		localCfg.IMAPUsername = ""
	}

	localCfg.Microsoft = microsoft
	localCfg.LoginConfig = microsoftCredentialConfigName
	localCfg.Drivers = append([]string(nil), drivers...)
	return localCfg, nil
}

func applyMicrosoftDriverAuthConfig(authCfg appconfig.AuthConfig, drivers []string) appconfig.AuthConfig {
	needsGraph := false
	needsEWS := false
	needsIMAP := false
	for _, driver := range uniqueNormalizedDrivers(drivers) {
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
	switch value {
	case "":
		return override, nil
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
	_, _ = fmt.Fprintln(out, "请选择 credential 需要绑定的邮件驱动，可输入编号或名称，多个值用逗号分隔。")
	for idx, driver := range candidates {
		registration, _ := lookupDriverRegistration(driver)
		configName := ""
		if registration.LoginConfig != nil {
			configName = registration.LoginConfig.Name
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
		if err == io.EOF {
			return nil, parseErr
		}
		_, _ = fmt.Fprintf(out, "驱动选择无效: %v\n", parseErr)
	}
}

func availableLoginDrivers() []string {
	drivers := make([]string, 0, len(driverRegistrations))
	for name, registration := range driverRegistrations {
		if registration.LoginConfig == nil {
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

func resolveDriverLoginConfig(drivers []string) (*driverLoginConfig, []string, error) {
	if len(drivers) == 0 {
		return nil, nil, nil
	}
	var selected *driverLoginConfig
	for _, driver := range uniqueNormalizedDrivers(drivers) {
		registration, ok := lookupDriverRegistration(driver)
		if !ok {
			return nil, nil, fmt.Errorf("不支持的 driver: %s", driver)
		}
		if registration.LoginConfig == nil {
			continue
		}
		if selected == nil {
			selected = registration.LoginConfig
			continue
		}
		if selected.Name != registration.LoginConfig.Name {
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
		registration, ok := lookupDriverRegistration(driver)
		if !ok {
			return nil, nil, fmt.Errorf("不支持的 driver: %s", driver)
		}
		if registration.RevokeConfig == nil {
			continue
		}
		if selected == nil {
			selected = registration.RevokeConfig
			continue
		}
		if selected.Name != registration.RevokeConfig.Name {
			return nil, nil, fmt.Errorf("driver %s 与 %s 需要不同的 revoke 配置，当前 credential 无法共享", drivers[0], driver)
		}
	}
	return selected, uniqueNormalizedDrivers(drivers), nil
}
