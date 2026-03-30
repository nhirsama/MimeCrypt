package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const localizedUsageTemplate = `用法:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

别名:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

示例:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

可用命令:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

其他命令:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

参数:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

全局参数:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

更多帮助主题:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

使用 "{{.CommandPath}} [command] --help" 查看更多信息。{{end}}
`

const localizedHelpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

const localizedHelpFlagUsage = "查看帮助"

func localizeCobraSupport(root *cobra.Command) {
	if root == nil {
		return
	}

	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	root.SetUsageTemplate(localizedUsageTemplate)
	root.SetHelpTemplate(localizedHelpTemplate)

	localizeHelpCommand(root)
	localizeCompletionCommand(root)
	localizeHelpFlags(root)
}

func localizeHelpCommand(root *cobra.Command) {
	helpCmd := findSubcommand(root, "help")
	if helpCmd == nil {
		return
	}

	root.SetHelpCommand(helpCmd)
	helpCmd.Short = "查看任意命令的帮助"
	helpCmd.Long = fmt.Sprintf(`查看应用中任意命令的帮助。
直接执行 %s help [命令路径] 可查看完整说明。`, root.Name())
}

func localizeCompletionCommand(root *cobra.Command) {
	completionCmd := findSubcommand(root, "completion")
	if completionCmd == nil {
		return
	}

	rootName := root.Name()
	completionCmd.Short = "生成指定 shell 的自动补全脚本"
	completionCmd.Long = fmt.Sprintf(`为 %s 生成指定 shell 的自动补全脚本。
查看各子命令的帮助以了解脚本安装方式。`, rootName)

	for _, shell := range completionCmd.Commands() {
		switch shell.Name() {
		case "bash":
			shell.Short = "为 bash 生成自动补全脚本"
			shell.Long = fmt.Sprintf(`为 bash 生成自动补全脚本。

该脚本依赖 bash-completion。
如果尚未安装，请先通过系统包管理器安装。

在当前 shell 会话中加载补全：

	source <(%[1]s completion bash)

为每个新会话启用补全，执行一次：

#### Linux:

	%[1]s completion bash > /etc/bash_completion.d/%[1]s

#### macOS:

	%[1]s completion bash > $(brew --prefix)/etc/bash_completion.d/%[1]s

配置生效后需要重新启动 shell。`, rootName)
		case "zsh":
			shell.Short = "为 zsh 生成自动补全脚本"
			shell.Long = fmt.Sprintf(`为 zsh 生成自动补全脚本。

如果当前环境尚未启用 shell 补全，需要先启用一次：

	echo "autoload -U compinit; compinit" >> ~/.zshrc

在当前 shell 会话中加载补全：

	source <(%[1]s completion zsh)

为每个新会话启用补全，执行一次：

#### Linux:

	%[1]s completion zsh > "${fpath[1]}/_%[1]s"

#### macOS:

	%[1]s completion zsh > $(brew --prefix)/share/zsh/site-functions/_%[1]s

配置生效后需要重新启动 shell。`, rootName)
		case "fish":
			shell.Short = "为 fish 生成自动补全脚本"
			shell.Long = fmt.Sprintf(`为 fish 生成自动补全脚本。

在当前 shell 会话中加载补全：

	%[1]s completion fish | source

为每个新会话启用补全，执行一次：

	%[1]s completion fish > ~/.config/fish/completions/%[1]s.fish

配置生效后需要重新启动 shell。`, rootName)
		case "powershell":
			shell.Short = "为 PowerShell 生成自动补全脚本"
			shell.Long = fmt.Sprintf(`为 PowerShell 生成自动补全脚本。

在当前 shell 会话中加载补全：

	%[1]s completion powershell | Out-String | Invoke-Expression

为每个新会话启用补全，请将上面命令的输出加入 PowerShell profile。`, rootName)
		}

		if flag := shell.Flags().Lookup("no-descriptions"); flag != nil {
			flag.Usage = "禁用补全说明"
		}
	}
}

func localizeHelpFlags(root *cobra.Command) {
	walkCommands(root, func(cmd *cobra.Command) {
		if cmd == nil {
			return
		}
		cmd.InitDefaultHelpFlag()
		if flag := cmd.Flags().Lookup("help"); flag != nil {
			flag.Usage = localizedHelpFlagUsage
		}
	})
}

func walkCommands(root *cobra.Command, visit func(*cobra.Command)) {
	if root == nil || visit == nil {
		return
	}

	visit(root)
	for _, cmd := range root.Commands() {
		walkCommands(cmd, visit)
	}
}

func findSubcommand(root *cobra.Command, name string) *cobra.Command {
	if root == nil {
		return nil
	}
	for _, cmd := range root.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}
