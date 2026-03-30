package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/interact"
	"mimecrypt/internal/providers"
)

const defaultSourceName = "default"

type sourceConfigDraft struct {
	Source        appconfig.Source
	RouteName     string
	TargetSinkRef string
}

func newConfigCmd() *cobra.Command {
	bootstrap := loadCommandConfigBootstrap()

	cmd := &cobra.Command{
		Use:   "config",
		Short: "交互式管理 topology 配置",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := bootstrap.Error(); err != nil {
				return fmt.Errorf("config 失败: %w", err)
			}
			return runConfigMenu(cmd, bootstrap.Config())
		},
	}
	cmd.AddCommand(newConfigSourceCmd())
	return cmd
}

func newConfigSourceCmd() *cobra.Command {
	bootstrap := loadCommandConfigBootstrap()
	topologyFlags := newTopologyConfigFlags(bootstrap.Config())

	cmd := &cobra.Command{
		Use:   "source [source-name]",
		Short: "交互式配置 source 并写入 topology",
		Args:  argRange(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bootstrap.Error(); err != nil {
				return fmt.Errorf("config source 失败: %w", err)
			}
			cfg := topologyFlags.apply(bootstrap.Config())
			explicitSourceName := ""
			if len(args) == 1 {
				explicitSourceName = strings.TrimSpace(args[0])
			}
			return runSourceConfigFlow(cmd, cfg, explicitSourceName, strings.TrimSpace(topologyFlags.routeName))
		},
	}

	topologyFlags.addFlags(cmd)
	return cmd
}

func runConfigMenu(cmd *cobra.Command, cfg appconfig.Config) error {
	out := cmd.OutOrStdout()
	reader := bufio.NewReader(cmd.InOrStdin())

	_, _ = fmt.Fprintln(out, "配置菜单")
	_, _ = fmt.Fprintln(out, "  s) 配置 source")
	_, _ = fmt.Fprintln(out, "  q) 退出配置")

	choice, err := interact.PromptMenuChoice(reader, out, "操作", []string{"s", "q"}, "q")
	if err != nil {
		if errors.Is(err, interact.ErrAbort) {
			_, _ = fmt.Fprintln(out, "已退出配置")
			return nil
		}
		return fmt.Errorf("读取配置菜单失败: %w", err)
	}

	switch choice {
	case "s":
		return runSourceConfigFlow(cmd, cfg, "", "")
	default:
		_, _ = fmt.Fprintln(out, "已退出配置")
		return nil
	}
}

func runSourceConfigFlow(cmd *cobra.Command, cfg appconfig.Config, explicitSourceName, explicitRouteName string) error {
	topologyPath := strings.TrimSpace(cfg.TopologyPath)
	if topologyPath == "" {
		topologyPath = appconfig.DefaultTopologyPath(cfg.Auth.StateDir)
	}

	topology, err := loadOrInitTopology(topologyPath)
	if err != nil {
		return fmt.Errorf("加载 topology 失败: %w", err)
	}

	updated, draft, err := promptSourceConfig(cmd.InOrStdin(), cmd.OutOrStdout(), topology, explicitSourceName, explicitRouteName)
	if err != nil {
		if errors.Is(err, interact.ErrAbort) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "已取消 source 配置")
			return nil
		}
		return fmt.Errorf("配置 source 失败: %w", err)
	}

	if err := appconfig.SaveTopologyFile(topologyPath, updated); err != nil {
		return fmt.Errorf("保存 topology 失败: %w", err)
	}

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "已写入 source 配置，topology=%s\n", topologyPath)
	for _, line := range providers.DescribeSourceConfig(draft.Source) {
		_, _ = fmt.Fprintf(out, "%s\n", line)
	}
	if strings.TrimSpace(draft.RouteName) != "" {
		_, _ = fmt.Fprintf(out, "route=%s\n", draft.RouteName)
	}
	return nil
}

func loadOrInitTopology(path string) (appconfig.Topology, error) {
	topology, err := appconfig.LoadTopologyFile(path)
	if err == nil {
		return topology.Normalize(), nil
	}
	if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file or directory") {
		return appconfig.Topology{}, nil
	}
	return appconfig.Topology{}, err
}

func promptSourceConfig(in io.Reader, out io.Writer, topology appconfig.Topology, explicitSourceName, explicitRouteName string) (appconfig.Topology, sourceConfigDraft, error) {
	if in == nil {
		in = strings.NewReader("")
	}
	if out == nil {
		out = io.Discard
	}
	reader := bufio.NewReader(in)
	draft := defaultSourceConfigDraft(topology, explicitSourceName, explicitRouteName)

	for {
		_, _ = fmt.Fprintln(out, "Source 配置（输入 q 可退出，回车保留当前值）")

		sourceName, err := interact.PromptString(reader, out, "Source Name", draft.Source.Name, false)
		if err != nil {
			return appconfig.Topology{}, sourceConfigDraft{}, err
		}
		draft.Source.Name = sourceName

		driver, err := promptSourceDriver(reader, out, draft.Source.Driver)
		if err != nil {
			return appconfig.Topology{}, sourceConfigDraft{}, err
		}
		draft.Source.Driver = driver

		existing := topology.Sources[draft.Source.Name]
		existing.Name = draft.Source.Name
		existing.Driver = driver
		configuredSource, err := providers.ConfigureSourceConfig(driver, existing, reader, out)
		if err != nil {
			return appconfig.Topology{}, sourceConfigDraft{}, err
		}
		draft.Source = configuredSource

		if len(topology.Routes) > 0 || len(topology.Sinks) > 0 || strings.TrimSpace(draft.RouteName) != "" {
			printExistingTopologyRefs(out, "可用 route", mapKeys(topology.Routes))
			routeName, err := interact.PromptString(reader, out, "Route Name（留空仅保存 source）", draft.RouteName, true)
			if err != nil {
				return appconfig.Topology{}, sourceConfigDraft{}, err
			}
			draft.RouteName = routeName
		}

		if strings.TrimSpace(draft.RouteName) != "" {
			if _, ok := topology.Routes[draft.RouteName]; !ok {
				sinks := mapKeys(topology.Sinks)
				if len(sinks) == 0 {
					return appconfig.Topology{}, sourceConfigDraft{}, fmt.Errorf("topology 当前没有可用 sink，无法创建 route %s", draft.RouteName)
				}
				printExistingTopologyRefs(out, "可用 sink", sinks)
				sinkRef, err := interact.PromptString(reader, out, "Target Sink Ref", firstNonEmpty(strings.TrimSpace(draft.TargetSinkRef), sinks[0]), false)
				if err != nil {
					return appconfig.Topology{}, sourceConfigDraft{}, err
				}
				draft.TargetSinkRef = sinkRef
			} else {
				draft.TargetSinkRef = ""
			}
		} else {
			draft.TargetSinkRef = ""
		}

		printSourceDraftSummary(out, draft, topology)
		action, err := interact.PromptMenuChoice(reader, out, "确认（y 保存 / e 重新编辑 / q 取消）", []string{"y", "e", "q"}, "y")
		if err != nil {
			return appconfig.Topology{}, sourceConfigDraft{}, err
		}
		switch action {
		case "e":
			continue
		case "q":
			return appconfig.Topology{}, sourceConfigDraft{}, interact.ErrAbort
		default:
			updated, applyErr := applySourceDraft(topology, draft)
			if applyErr != nil {
				return appconfig.Topology{}, sourceConfigDraft{}, applyErr
			}
			return updated, draft, nil
		}
	}
}

func defaultSourceConfigDraft(topology appconfig.Topology, explicitSourceName, explicitRouteName string) sourceConfigDraft {
	sourceName := strings.TrimSpace(explicitSourceName)
	source := appconfig.Source{}
	if sourceName != "" {
		source = topology.Sources[sourceName]
	} else if defaultSource, err := topology.DefaultSourceConfig(); err == nil {
		sourceName = strings.TrimSpace(defaultSource.Name)
		source = defaultSource
	} else {
		for _, candidate := range mapKeys(topology.Sources) {
			existing := topology.Sources[candidate]
			if existing.Driver == "" {
				continue
			}
			sourceName = strings.TrimSpace(existing.Name)
			source = existing
			break
		}
	}
	if sourceName == "" {
		sourceName = defaultSourceName
	}
	source.Name = sourceName

	draft := sourceConfigDraft{
		Source:    source,
		RouteName: strings.TrimSpace(explicitRouteName),
	}
	if draft.RouteName == "" {
		draft.RouteName, draft.TargetSinkRef = findSourceRouteDefaults(topology, sourceName)
	}
	if draft.RouteName == "" && len(topology.Sinks) > 0 {
		draft.RouteName = firstNonEmpty(strings.TrimSpace(topology.DefaultRoute), "default")
	}
	if draft.TargetSinkRef == "" {
		sinks := mapKeys(topology.Sinks)
		if len(sinks) == 1 {
			draft.TargetSinkRef = sinks[0]
		}
	}
	return draft
}

func findSourceRouteDefaults(topology appconfig.Topology, sourceName string) (string, string) {
	sourceName = strings.TrimSpace(sourceName)
	if sourceName == "" {
		return "", ""
	}
	for _, routeName := range mapKeys(topology.Routes) {
		route := topology.Routes[routeName]
		for _, ref := range route.SourceRefs {
			if strings.TrimSpace(ref) != sourceName {
				continue
			}
			sinkRef := ""
			if len(route.Targets) > 0 {
				sinkRef = strings.TrimSpace(route.Targets[0].SinkRef)
			}
			return strings.TrimSpace(route.Name), sinkRef
		}
	}
	return "", ""
}

func promptSourceDriver(reader *bufio.Reader, out io.Writer, current string) (string, error) {
	drivers := providers.ConfigurableSourceDrivers()
	if len(drivers) == 0 {
		return "", fmt.Errorf("当前没有可交互配置的 source driver")
	}

	_, _ = fmt.Fprintln(out, "可配置的 source driver:")
	for idx, driver := range drivers {
		_, _ = fmt.Fprintf(out, "  %d) %s\n", idx+1, driver)
	}

	for {
		value, err := interact.PromptString(reader, out, "Driver", current, false)
		if err != nil {
			return "", err
		}
		key := strings.ToLower(strings.TrimSpace(value))
		for idx, driver := range drivers {
			if key == driver || key == fmt.Sprintf("%d", idx+1) {
				return driver, nil
			}
		}
		_, _ = fmt.Fprintf(out, "不支持的 source driver: %s\n", value)
	}
}

func applySourceDraft(topology appconfig.Topology, draft sourceConfigDraft) (appconfig.Topology, error) {
	topology = topology.Normalize()
	if topology.Sources == nil {
		topology.Sources = map[string]appconfig.Source{}
	}
	if topology.Routes == nil {
		topology.Routes = map[string]appconfig.Route{}
	}

	if err := draft.Source.Validate(draft.Source.Name, topology.Credentials); err != nil {
		return appconfig.Topology{}, err
	}
	topology.Sources[draft.Source.Name] = draft.Source

	if strings.TrimSpace(draft.RouteName) != "" {
		route, exists := topology.Routes[draft.RouteName]
		if !exists {
			if strings.TrimSpace(draft.TargetSinkRef) == "" {
				return appconfig.Topology{}, fmt.Errorf("新建 route %s 时必须提供 target sink", draft.RouteName)
			}
			route = appconfig.Route{
				Name:       draft.RouteName,
				SourceRefs: []string{draft.Source.Name},
				Targets: []appconfig.RouteTarget{
					{
						Name:     draft.TargetSinkRef,
						SinkRef:  draft.TargetSinkRef,
						Artifact: "primary",
						Required: true,
					},
				},
			}
		} else {
			route.SourceRefs = appendUniqueRef(route.SourceRefs, draft.Source.Name)
		}
		if err := route.Validate(draft.RouteName, topology.Sources, topology.Sinks); err != nil {
			return appconfig.Topology{}, err
		}
		topology.Routes[draft.RouteName] = route
		if strings.TrimSpace(topology.DefaultRoute) == "" {
			topology.DefaultRoute = draft.RouteName
		}
	}

	if strings.TrimSpace(topology.DefaultSource) == "" {
		topology.DefaultSource = draft.Source.Name
	}

	return topology.Normalize(), nil
}

func printSourceDraftSummary(out io.Writer, draft sourceConfigDraft, topology appconfig.Topology) {
	_, _ = fmt.Fprintln(out, "当前配置摘要")
	for _, line := range providers.DescribeSourceConfig(draft.Source) {
		_, _ = fmt.Fprintf(out, "  %s\n", line)
	}
	if strings.TrimSpace(draft.RouteName) == "" {
		_, _ = fmt.Fprintln(out, "  route=(未挂到 route)")
		return
	}
	if _, ok := topology.Routes[draft.RouteName]; ok {
		_, _ = fmt.Fprintf(out, "  route=%s (existing)\n", draft.RouteName)
		return
	}
	_, _ = fmt.Fprintf(out, "  route=%s (new, target=%s)\n", draft.RouteName, draft.TargetSinkRef)
}

func printExistingTopologyRefs(out io.Writer, label string, refs []string) {
	if len(refs) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "%s: %s\n", label, strings.Join(refs, ", "))
}

func mapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, strings.TrimSpace(key))
	}
	sort.Strings(keys)
	return keys
}

func appendUniqueRef(values []string, candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	for _, existing := range values {
		if strings.TrimSpace(existing) == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
