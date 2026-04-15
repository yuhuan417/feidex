package app

import (
	"fmt"
	"strings"

	"feidex/internal/feishu"
)

type localCommandSpec struct {
	Names       []string
	IsLocal     func(fields []string) bool
	Handle      func(a *App, msg *feishu.InboundMessage, args []string) error
	HelpGroup   string
	HelpEntries []helpCommandSpec
}

func localCommandSpecList() []localCommandSpec {
	return []localCommandSpec{
		{
			Names: []string{"/menu"},
			IsLocal: func([]string) bool {
				return true
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
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandHelp(msg, args)
			},
			HelpEntries: []helpCommandSpec{
				{Command: "/help", Summary: "查看所有本地命令与说明。"},
			},
		},
		{
			Names: []string{"/history"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandHistory(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/history", Summary: "查看当前 thread 的 turn 历史记录，重点展示每个 turn 的输入。"},
			},
		},
		{
			Names: []string{"/usage"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandUsage(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/usage", Summary: "查看当前 thread 的累计 token usage。"},
			},
		},
		{
			Names: []string{"/model"},
			IsLocal: func(fields []string) bool {
				return len(fields) == 1
			},
			Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
				return a.commandModel(msg)
			},
			HelpGroup: "model",
			HelpEntries: []helpCommandSpec{
				{Command: "/model", Summary: "打开模型选择与推理强度配置。"},
			},
		},
		{
			Names: []string{"/quiet"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandQuiet(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "`/quiet`", Summary: "切换 Quiet 模式。"},
				{Command: "/quiet on", Summary: "开启 Quiet 模式。"},
				{Command: "/quiet off", Summary: "关闭 Quiet 模式。"},
			},
		},
		{
			Names: []string{"/debug"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandDebug(msg, args)
			},
			HelpGroup: "system",
			HelpEntries: []helpCommandSpec{
				{Command: "/debug", Summary: "切换服务端 slog 日志级别（debug/info）。"},
				{Command: "/debug on", Summary: "切换到 debug 级别。"},
				{Command: "/debug off", Summary: "切换到 info 级别。"},
				{Command: "/debug logs", Summary: "查看最近一段服务端 slog 日志。"},
			},
		},
		{
			Names: []string{"/fast"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandFast(msg, args)
			},
			HelpGroup: "model",
			HelpEntries: []helpCommandSpec{
				{Command: "/fast", Summary: "切换当前线程的响应速度设置。"},
			},
		},
		{
			Names: []string{"/download"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandDownload(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/download", Summary: "在当前 workspace 范围内选择文件，并生成下载链接。"},
			},
		},
		{
			Names: []string{"/compact"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandCompact(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/compact", Summary: "压缩当前线程的上下文，减少上下文占用。"},
			},
		},
		{
			Names: []string{"/fork"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandFork(msg, args)
			},
			HelpGroup: "thread",
			HelpEntries: []helpCommandSpec{
				{Command: "/fork", Summary: "等价于 `/thread fork`。"},
			},
		},
		{
			Names: []string{"/new"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
				return a.commandNew(msg)
			},
			HelpGroup: "thread",
			HelpEntries: []helpCommandSpec{
				{Command: "/new", Summary: "等价于 `/thread new`。"},
			},
		},
		{
			Names: []string{"/thread"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandThread(msg, args)
			},
			HelpGroup: "thread",
			HelpEntries: []helpCommandSpec{
				{Command: "/thread", Summary: "打开 thread 菜单。"},
				{Command: "/thread list", Summary: "查看当前工作区可恢复的线程。"},
				{Command: "/thread list all", Summary: "查看更多来源的线程，仅保留命令入口。"},
				{Command: "/thread new", Summary: "立即创建并切换到新的 thread。"},
				{Command: "/thread fork", Summary: "复制当前 thread 为一个新分支，并立即切换过去。"},
				{Command: "/thread sandbox", Summary: "配置当前线程的 sandbox。"},
				{Command: "/thread policy", Summary: "配置当前线程的 approval policy。"},
			},
		},
		{
			Names: []string{"/threads"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				if len(args) > 0 {
					return fmt.Errorf("usage: /threads")
				}
				return a.commandThread(msg, []string{"list"})
			},
			HelpGroup: "thread",
			HelpEntries: []helpCommandSpec{
				{Command: "/threads", Summary: "等价于 `/thread list`。"},
			},
		},
		{
			Names: []string{"/interrupt", "/stop"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
				return a.commandInterrupt(msg)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "`/interrupt` 或 `/stop`", Summary: "中断当前运行中的任务，并清空排队/暂存输入。"},
			},
		},
		{
			Names: []string{"/status"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
				return a.commandStatus(msg)
			},
			HelpGroup: "system",
			HelpEntries: []helpCommandSpec{
				{Command: "/status", Summary: "查看当前会话、线程、工作区与模型状态。"},
			},
		},
		{
			Names: []string{"/upgrade"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandUpgrade(msg, args)
			},
			HelpGroup: "system",
			HelpEntries: []helpCommandSpec{
				{Command: "/upgrade", Summary: "检查最新版本并发起升级。"},
				{Command: "/upgrade v0.3.0", Summary: "跳过最新版本检查，直接升级到指定版本。"},
				{Command: "/upgrade local", Summary: "打开当前 workspace 的本地 Binary 选择器。"},
				{Command: "/upgrade path ./dist/feidex-linux-amd64", Summary: "直接用当前 workspace 下的本地 Binary 发起升级确认。"},
			},
		},
		{
			Names: []string{"/workspace"},
			IsLocal: func([]string) bool {
				return true
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandWorkspace(msg, args)
			},
			HelpGroup: "workspace",
			HelpEntries: []helpCommandSpec{
				{Command: "/workspace", Summary: "打开工作区菜单。"},
				{Command: "/workspace list", Summary: "打开工作区列表并可直接切换。"},
				{Command: "/workspace new", Summary: "创建新工作区。"},
				{Command: "/workspace use ID", Summary: "切换到指定工作区。"},
				{Command: "/workspace sandbox", Summary: "配置当前工作区默认 sandbox。"},
				{Command: "/workspace policy", Summary: "配置当前工作区默认 approval policy。"},
			},
		},
	}
}

var helpGroupOrder = []string{
	"常用工具",
	"model",
	"thread",
	"workspace",
	"system",
}

func findLocalCommandSpec(name string) *localCommandSpec {
	specs := localCommandSpecList()
	for i := range specs {
		spec := &specs[i]
		for _, candidate := range spec.Names {
			if candidate == name {
				return spec
			}
		}
	}
	return nil
}

func renderHelpBodyFromRegistry() string {
	lines := []string{"命令说明：", ""}
	intro := make([]helpCommandSpec, 0, 2)
	sections := map[string][]helpCommandSpec{}
	for _, spec := range localCommandSpecList() {
		if len(spec.HelpEntries) == 0 {
			continue
		}
		if strings.TrimSpace(spec.HelpGroup) == "" {
			intro = append(intro, spec.HelpEntries...)
			continue
		}
		group := strings.TrimSpace(spec.HelpGroup)
		sections[group] = append(sections[group], spec.HelpEntries...)
	}
	lines = appendHelpCommands(lines, intro)
	for _, group := range helpGroupOrder {
		specs := sections[group]
		if len(specs) == 0 {
			continue
		}
		lines = append(lines, "", group+"：")
		lines = appendHelpCommands(lines, specs)
	}
	return strings.Join(lines, "\n")
}
