package app

import "feidex/internal/feishu"

func localCommandSystemIntroSpecs() []localCommandSpec {
	return []localCommandSpec{
		{
			Names: []string{"/menu"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
				return a.sendCommandMenu(msg)
			},
			HelpEntries: []helpCommandSpec{
				{Command: "/menu", Summary: "打开命令菜单。"},
			},
		},
		{
			Names: []string{"/help"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return newCommandService(a).commandHelp(msg, args)
			},
			HelpEntries: []helpCommandSpec{
				{Command: "/help", Summary: "查看所有本地命令与说明。"},
			},
		},
		{
			Names: []string{"/backend"},
			IsLocal: func(fields []string) bool {
				return matchBackendCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return newBackendSelectionService(a).commandBackend(msg, args)
			},
			HelpGroup: "system",
			HelpEntries: []helpCommandSpec{
				{Command: "/backend", Summary: "查看本 frontend 当前可用的 backend，并在空闲态切换。"},
				{Command: "/backend retry", Summary: "查看当前 frontend 的自动重试开关。"},
				{Command: "/backend retry on", Summary: "开启当前 frontend 的自动重试。"},
				{Command: "/backend retry off", Summary: "关闭当前 frontend 的自动重试。"},
			},
		},
	}
}

func localCommandSystemDebugSpecs() []localCommandSpec {
	return []localCommandSpec{
		{
			Names: []string{"/debug"},
			IsLocal: func(fields []string) bool {
				return exactOrSingleArgCommand(fields, "on", "off", "logs")
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return newDebugService(a).commandDebug(msg, args)
			},
			HelpGroup: "system",
			HelpEntries: []helpCommandSpec{
				{Command: "/debug", Summary: "切换服务端 slog 日志级别（debug/info）。"},
				{Command: "/debug on", Summary: "切换到 debug 级别。"},
				{Command: "/debug off", Summary: "切换到 info 级别。"},
				{Command: "/debug logs", Summary: "查看最近一段服务端 slog 日志。"},
			},
		},
	}
}

func localCommandSystemRuntimeSpecs() []localCommandSpec {
	return []localCommandSpec{
		{
			Names: []string{"/status"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
				return a.commandStatus(msg)
			},
			HelpGroup: "system",
			HelpEntries: []helpCommandSpec{
				{Command: "/status", Summary: "查看当前会话、线程、工作区与模型状态。"},
			},
			Backends: backendCommandPolicy(backendClaude, partialBackendCommand(exactCommand, []helpCommandSpec{
				{Command: "/status", Summary: "查看当前会话、工作区、Claude 模型与权限状态。"},
			})),
		},
		{
			Names: []string{"/codex"},
			IsLocal: func(fields []string) bool {
				return matchCodexCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return newBackendUpgradeService(a).commandCodex(msg, args)
			},
			HelpGroup: "system",
			HelpEntries: []helpCommandSpec{
				{Command: "/codex", Summary: "查看本机 Codex CLI 的安装与升级状态。"},
				{Command: "/codex check", Summary: "检查 npm 官方最新稳定版。"},
				{Command: "/codex upgrade", Summary: "准备升级到 npm 官方最新稳定版，并支持 smoke test 失败自动回滚。"},
				{Command: "/codex restart", Summary: "在空闲态原地重启 Codex runtime，适合刷新新安装的 Skill。"},
			},
		},
		{
			Names: []string{"/claude"},
			IsLocal: func(fields []string) bool {
				return matchClaudeCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return newBackendUpgradeService(a).commandClaude(msg, args)
			},
			HelpGroup: "system",
			HelpEntries: []helpCommandSpec{
				{Command: "/claude", Summary: "查看本机 Claude CLI 的安装与升级状态。"},
				{Command: "/claude check", Summary: "检查 npm 官方最新稳定版。"},
				{Command: "/claude upgrade", Summary: "准备升级到 npm 官方最新稳定版，失败自动回滚。"},
				{Command: "/claude restart", Summary: "在空闲态原地重启 Claude runtime。"},
			},
		},
		{
			Names: []string{"/upgrade"},
			IsLocal: func(fields []string) bool {
				return matchUpgradeCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return newAppUpgradeService(a).commandUpgrade(msg, args)
			},
			HelpGroup: "system",
			HelpEntries: []helpCommandSpec{
				{Command: "/upgrade", Summary: "检查最新版本并发起升级。"},
				{Command: "/upgrade dev", Summary: "升级到 `dev-latest` 当前指向的开发版构建。"},
				{Command: "/upgrade v0.3.0", Summary: "跳过最新版本检查，直接升级到指定版本。"},
				{Command: "/upgrade local", Summary: "打开当前 workspace 的本地 Binary 选择器。"},
				{Command: "/upgrade path ./dist/feidex-linux-amd64", Summary: "直接用当前 workspace 下的本地 Binary 发起升级确认。"},
			},
		},
	}
}
