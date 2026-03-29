package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/flowruntime"
	"mimecrypt/internal/modules/list"
	"mimecrypt/internal/provider"
)

func newListCmd() *cobra.Command {
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return newErrorCommand("list", "列出最新邮件摘要", err)
	}

	providerFlags := newProviderConfigFlags(cfg)
	topologyFlags := newTopologyConfigFlags(cfg)
	folder := cfg.Mail.Sync.Folder

	cmd := &cobra.Command{
		Use:   "list <end> | list <start> <end>",
		Short: "列出指定文件夹中最近一段邮件摘要",
		Long:  "列出指定文件夹中按接收时间倒序排列的一段邮件摘要，范围语义使用半开区间 [start,end)。",
		Example: strings.Join([]string{
			"mimecrypt list 10",
			"mimecrypt list 10 20",
			"mimecrypt list 0 5 --folder inbox",
		}, "\n"),
		Args: argRange(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg = providerFlags.apply(cfg, cmd)
			cfg = topologyFlags.apply(cfg)
			cfg.Mail.Sync.Folder = folder

			start, end, err := parseLatestRange(args)
			if err != nil {
				return fmt.Errorf("list 失败: %w", err)
			}

			request := list.Request{
				Folder: cfg.Mail.Sync.Folder,
				Start:  start,
				End:    end,
			}
			var service *list.Service
			if strings.TrimSpace(cfg.TopologyPath) == "" {
				if strings.TrimSpace(cfg.Mail.Sync.Folder) == "" {
					return fmt.Errorf("list 失败: folder 不能为空")
				}
				service, err = buildListService(cfg)
				if err != nil {
					return fmt.Errorf("list 失败: %w", err)
				}
			} else {
				resolved, err := resolveTopologySource(cfg, topologyFlags)
				if err != nil {
					return fmt.Errorf("list 失败: %w", err)
				}
				if resolved.Custom && cmd.Flags().Changed("folder") {
					return fmt.Errorf("list 失败: --folder 与 --topology-file 不能同时覆盖 source 文件夹")
				}

				service, err = flowruntime.BuildListService(resolved.SourcePlan)
				if err != nil {
					return fmt.Errorf("list 失败: %w", err)
				}
				request.Folder = resolved.Source.Folder
			}

			result, err := service.Run(cmd.Context(), request)
			if err != nil {
				return fmt.Errorf("list 失败: %w", err)
			}

			if len(result.Messages) == 0 {
				fmt.Printf("未找到邮件，folder=%s range=%d..%d\n", result.Folder, result.Start, result.End)
				return nil
			}

			fmt.Println("index\treceived_at\tmessage_id\tsubject")
			for idx, message := range result.Messages {
				fmt.Printf("%d\t%s\t%s\t%q\n", result.Start+idx, formatMessageTime(message), message.ID, message.Subject)
			}
			return nil
		},
	}

	providerFlags.addFlags(cmd)
	topologyFlags.addSourceFlags(cmd)
	cmd.Flags().StringVar(&folder, "folder", folder, "待列出的邮件文件夹标识；Graph 使用 folder id，IMAP 使用 mailbox 名称")

	return cmd
}

func parseLatestRange(args []string) (int, int, error) {
	switch len(args) {
	case 1:
		end, err := parseNonNegativeIndex(args[0], "end")
		if err != nil {
			return 0, 0, err
		}
		if end == 0 {
			return 0, 0, fmt.Errorf("end 必须大于 0")
		}
		return 0, end, nil
	case 2:
		start, err := parseNonNegativeIndex(args[0], "start")
		if err != nil {
			return 0, 0, err
		}
		end, err := parseNonNegativeIndex(args[1], "end")
		if err != nil {
			return 0, 0, err
		}
		if end <= start {
			return 0, 0, fmt.Errorf("end 必须大于 start")
		}
		return start, end, nil
	default:
		return 0, 0, fmt.Errorf("范围参数数量应为 1 个或 2 个")
	}
}

func parseNonNegativeIndex(value, name string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s 必须是非负整数", name)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s 不能小于 0", name)
	}
	return parsed, nil
}

func formatMessageTime(message provider.Message) string {
	if message.ReceivedDateTime.IsZero() {
		return "-"
	}
	return message.ReceivedDateTime.UTC().Format(time.RFC3339)
}
