package providers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"mimecrypt/internal/appconfig"
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
	AuthHints     []string
	RuntimeName   string
}

type LoginRuntime = CredentialRuntime

func BuildLoginRuntime(cfg appconfig.Config, hints ...string) (LoginRuntime, error) {
	resolvedHints := effectiveCredentialAuthHints(hints...)
	loginConfig, runtimeName, err := resolveCredentialRuntimeConfigForConfig(appconfig.CredentialKindOAuth, "")
	if err != nil {
		return LoginRuntime{}, err
	}

	effectiveCfg := cfg
	if loginConfig.ApplyConfig != nil {
		effectiveCfg = loginConfig.ApplyConfig(cfg, resolvedHints)
	}

	session, err := loginConfig.BuildSession(effectiveCfg)
	if err != nil {
		return LoginRuntime{}, err
	}

	var identityProbe func(context.Context) (provider.User, error)
	if loginConfig.BuildIdentityProbe != nil {
		identityProbe, err = loginConfig.BuildIdentityProbe(effectiveCfg, resolvedHints, session)
		if err != nil {
			return LoginRuntime{}, err
		}
	}

	return LoginRuntime{
		Kind:          normalizedCredentialKind(appconfig.CredentialKindOAuth),
		Config:        effectiveCfg,
		Session:       session,
		IdentityProbe: identityProbe,
		AuthHints:     resolvedHints,
		RuntimeName:   runtimeName,
	}, nil
}

func BuildCredentialRuntime(name, kind, runtimeName string, cfg appconfig.Config, hints ...string) (CredentialRuntime, error) {
	resolvedHints := effectiveCredentialAuthHints(hints...)
	loginConfig, runtimeName, err := resolveCredentialRuntimeConfig(kind, runtimeName)
	if err != nil {
		return CredentialRuntime{}, err
	}

	effectiveCfg := cfg
	if loginConfig.ApplyConfig != nil {
		effectiveCfg = loginConfig.ApplyConfig(cfg, resolvedHints)
	}

	session, err := loginConfig.BuildSession(effectiveCfg)
	if err != nil {
		return CredentialRuntime{}, err
	}

	var identityProbe func(context.Context) (provider.User, error)
	if loginConfig.BuildIdentityProbe != nil {
		identityProbe, err = loginConfig.BuildIdentityProbe(effectiveCfg, resolvedHints, session)
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
		AuthHints:     resolvedHints,
		RuntimeName:   runtimeName,
	}, nil
}

func ConfigureLoginLocalConfig(kind string, cfg appconfig.Config, localCfg appconfig.LocalConfig, in io.Reader, out io.Writer, hints ...string) (appconfig.LocalConfig, appconfig.Config, []string, error) {
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

	resolvedHints := effectiveCredentialAuthHints(hints...)
	storedRuntimeName := localCfg.EffectiveRuntimeName()
	storedHints := localCfg.AuthHintNames()
	if len(resolvedHints) == 0 {
		if storedRuntimeName != "" {
			resolvedHints = effectiveCredentialAuthHints(storedHints...)
		} else {
			_, _ = fmt.Fprintln(out, "请选择 credential 需要启用的认证提示，用于补全最小 scopes 和协议参数，输入 q 可退出。")
			var err error
			resolvedHints, err = promptCredentialAuthHintSelection(reader, out, storedHints)
			if err != nil {
				return appconfig.LocalConfig{}, appconfig.Config{}, nil, err
			}
		}
	}

	loginConfig, runtimeName, err := resolveCredentialRuntimeConfigForConfig(kind, storedRuntimeName)
	if err != nil {
		return appconfig.LocalConfig{}, appconfig.Config{}, nil, err
	}

	updated := localCfg.WithRuntimeName(runtimeName).WithAuthHintNames(resolvedHints)
	if loginConfig.ConfigureLocal != nil {
		updated, err = loginConfig.ConfigureLocal(cfg, updated, resolvedHints, reader, out)
		if err != nil {
			return appconfig.LocalConfig{}, appconfig.Config{}, nil, err
		}
	}
	updated = updated.WithRuntimeName(runtimeName).WithAuthHintNames(resolvedHints)

	effectiveCfg := cfg.WithLocalConfig(updated)
	if loginConfig.ApplyConfig != nil {
		effectiveCfg = loginConfig.ApplyConfig(effectiveCfg, resolvedHints)
	}
	return updated, effectiveCfg, resolvedHints, nil
}

func BuildRemoteRevoker(kind, runtimeName string, cfg appconfig.Config, tokenSource provider.TokenSource, drivers ...string) (CredentialRemoteRevoker, appconfig.Config, error) {
	resolvedHints := effectiveCredentialAuthHints(drivers...)
	revokeConfig, _, err := resolveCredentialRevokeConfig(kind, runtimeName)
	if err != nil {
		return nil, appconfig.Config{}, err
	}

	effectiveCfg := cfg
	if revokeConfig.ApplyConfig != nil {
		effectiveCfg = revokeConfig.ApplyConfig(cfg, resolvedHints)
	}

	revoker, err := revokeConfig.BuildRemoteRevoker(effectiveCfg, resolvedHints, tokenSource)
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

func configureOAuthDeviceLocalConfig(cfg appconfig.Config, localCfg appconfig.LocalConfig, hints []string, in io.Reader, out io.Writer) (appconfig.LocalConfig, error) {
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

	hintSummary := strings.Join(hints, ",")
	if hintSummary == "" {
		hintSummary = "未指定"
	}
	_, _ = fmt.Fprintf(out, "配置 credential runtime: %s (hints=%s，输入 q 可退出)\n", oauthDeviceRuntimeName, hintSummary)

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
	hasHints := len(uniqueNormalizedCredentialAuthHints(hints)) > 0
	if containsCredentialAuthHint(hints, "imap") {
		imapUsername, err := promptConfigValue(reader, out, "IMAP Username", cfg.Mail.Client.IMAPUsername, microsoft.IMAPUsername)
		if err != nil {
			return appconfig.LocalConfig{}, err
		}
		microsoft.IMAPUsername = imapUsername
		localCfg.IMAPUsername = imapUsername
	} else if hasHints {
		microsoft.IMAPUsername = ""
		localCfg.IMAPUsername = ""
	}

	localCfg.Microsoft = microsoft
	return localCfg.WithRuntimeName(oauthDeviceRuntimeName).WithAuthHintNames(hints), nil
}

func effectiveCredentialAuthHints(hints ...string) []string {
	return uniqueNormalizedCredentialAuthHints(hints)
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

func promptCredentialAuthHintSelection(reader *bufio.Reader, out io.Writer, current []string) ([]string, error) {
	candidates := availableCredentialAuthHintNames()
	if len(candidates) == 0 {
		return nil, fmt.Errorf("当前没有可用的 credential 认证提示")
	}

	current = filterCredentialAuthHintsByCandidateSet(current, candidates)
	_, _ = fmt.Fprintln(out, "请选择 credential 需要启用的认证提示，可输入编号或名称，多个值用逗号分隔。")
	for idx, hint := range candidates {
		_, _ = fmt.Fprintf(out, "  %d) %s\n", idx+1, hint)
	}

	for {
		prompt := "提示"
		if len(current) > 0 {
			prompt += " [" + strings.Join(current, ",") + "]"
		}
		prompt += ": "
		if _, err := fmt.Fprint(out, prompt); err != nil {
			return nil, fmt.Errorf("输出认证提示选择失败: %w", err)
		}

		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("读取认证提示选择失败: %w", err)
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
				return nil, fmt.Errorf("至少需要选择一个认证提示")
			}
			_, _ = fmt.Fprintln(out, "至少需要选择一个认证提示。")
			continue
		}

		selected, parseErr := parseCredentialAuthHintSelection(selection, candidates)
		if parseErr == nil {
			return selected, nil
		}
		if errors.Is(parseErr, interact.ErrAbort) {
			return nil, parseErr
		}
		if err == io.EOF {
			return nil, parseErr
		}
		_, _ = fmt.Fprintf(out, "认证提示选择无效: %v\n", parseErr)
	}
}

func parseCredentialAuthHintSelection(selection string, candidates []string) ([]string, error) {
	tokens := strings.FieldsFunc(selection, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	if len(tokens) == 0 {
		return nil, fmt.Errorf("至少需要选择一个认证提示")
	}

	candidateIndex := make(map[string]string, len(candidates))
	for idx, candidate := range candidates {
		candidateIndex[strconv.Itoa(idx+1)] = candidate
		candidateIndex[normalizeCredentialAuthHintName(candidate)] = candidate
	}

	selected := make([]string, 0, len(tokens))
	for _, token := range tokens {
		key := normalizeCredentialAuthHintName(token)
		hint, ok := candidateIndex[key]
		if !ok {
			return nil, fmt.Errorf("不支持的认证提示: %s", strings.TrimSpace(token))
		}
		selected = append(selected, hint)
	}
	return uniqueNormalizedCredentialAuthHints(selected), nil
}
