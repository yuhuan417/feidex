package app

import (
	"fmt"
	"strconv"
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

func exactCommand(fields []string) bool {
	return len(fields) == 1
}

func exactOrSingleArgCommand(fields []string, allowed ...string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 2 {
		return false
	}
	return commandArgInSet(fields[1], allowed...)
}

func commandArgInSet(value string, allowed ...string) bool {
	value = strings.TrimSpace(value)
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func matchReviewCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "uncommitted", "uncommittedChanges":
		return len(fields) == 2
	case "base", "commit":
		return len(fields) == 2 || len(fields) == 3
	case "custom":
		return len(fields) >= 2
	default:
		return false
	}
}

func matchHistoryCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 3 || strings.TrimSpace(fields[1]) != "detail" {
		return false
	}
	value, err := strconv.Atoi(strings.TrimSpace(fields[2]))
	return err == nil && value > 0
}

func matchModelCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 3 {
		return false
	}
	switch strings.TrimSpace(fields[1]) {
	case "set", "effort":
		return strings.TrimSpace(fields[2]) != ""
	default:
		return false
	}
}

func matchThreadCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "list":
		return len(fields) == 2 || (len(fields) == 3 && strings.TrimSpace(fields[2]) == "all")
	case "new", "fork":
		return len(fields) == 2
	case "resume":
		return len(fields) == 3
	case "sandbox", "policy":
		return len(fields) == 2 || len(fields) == 3
	default:
		return false
	}
}

func matchUpgradeCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "dev", "local":
		return len(fields) == 2
	case "path":
		return len(fields) >= 3
	default:
		if len(fields) != 2 {
			return false
		}
		_, err := normalizeUpgradeVersion(fields[1])
		return err == nil
	}
}

func matchCodexCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	if len(fields) != 2 {
		return false
	}
	return commandArgInSet(fields[1], "check", "upgrade", "restart")
}

func matchWorkspaceCommand(fields []string) bool {
	if len(fields) == 1 {
		return true
	}
	switch strings.TrimSpace(fields[1]) {
	case "list", "new":
		return len(fields) == 2
	case "delete":
		return len(fields) == 2 || len(fields) == 3
	case "sandbox", "policy":
		return len(fields) == 2 || len(fields) == 3
	case "clone":
		_, _, _, err := parseWorkspaceCloneArgs(fields[1:])
		return err == nil
	case "use":
		return len(fields) == 3
	default:
		return false
	}
}

func localCommandSpecList() []localCommandSpec {
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
				return a.commandHelp(msg, args)
			},
			HelpEntries: []helpCommandSpec{
				{Command: "/help", Summary: "查看所有本地命令与说明。"},
			},
		},
		{
			Names: []string{"/history"},
			IsLocal: func(fields []string) bool {
				return matchHistoryCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandHistory(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/history", Summary: "查看当前 thread 的 turn 历史记录，重点展示每个 turn 的输入。"},
				{Command: "/history detail TURN_NUMBER", Summary: "直接查看指定 Turn # 的详情。"},
			},
		},
		{
			Names: []string{"/skills"},
			IsLocal: func(fields []string) bool {
				return matchSkillsCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandSkills(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/skills", Summary: "查看当前工作区可用的 skills，并选择下一条消息默认携带的 skill。"},
				{Command: "/skills reload", Summary: "强制刷新当前工作区的 skill 列表。"},
				{Command: "$skill-name <内容>", Summary: "以 skill 前缀显式指定本条消息使用的 skill。"},
			},
		},
		{
			Names: []string{"/usage"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
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
				return matchModelCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandModel(msg, args)
			},
			HelpGroup: "model",
			HelpEntries: []helpCommandSpec{
				{Command: "/model", Summary: "打开模型选择与推理强度配置。"},
				{Command: "/model set <model-id>", Summary: "直接设置全局 model。"},
				{Command: "/model set default", Summary: "清空全局 model，跟随 app-server 默认。"},
				{Command: "/model effort <effort>", Summary: "直接设置全局推理强度。"},
				{Command: "/model effort default", Summary: "清空全局推理强度，跟随模型默认。"},
			},
		},
		{
			Names: []string{"/quiet"},
			IsLocal: func(fields []string) bool {
				return exactOrSingleArgCommand(fields, "config", "verbose", "progress", "normal", "final")
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandQuiet(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/quiet", Summary: "打开 Quiet Mode 配置卡。"},
				{Command: "/quiet verbose", Summary: "完整展开所有过程消息。"},
				{Command: "/quiet progress", Summary: "把过程消息折叠成“工作中”卡。"},
				{Command: "/quiet normal", Summary: "只保留 plan 和 agent/final message。"},
				{Command: "/quiet final", Summary: "只保留 final message。"},
				{Command: "/quiet config", Summary: "打开 Quiet Mode 配置卡。"},
			},
		},
		{
			Names: []string{"/debug"},
			IsLocal: func(fields []string) bool {
				return exactOrSingleArgCommand(fields, "on", "off", "logs")
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
			IsLocal: func(fields []string) bool {
				return exactOrSingleArgCommand(fields, "config", "fast", "default", "off", "toggle")
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandFast(msg, args)
			},
			HelpGroup: "model",
			HelpEntries: []helpCommandSpec{
				{Command: "/fast", Summary: "切换当前线程的响应速度设置。"},
				{Command: "/fast fast", Summary: "将当前线程的 service tier 设为 fast。"},
				{Command: "/fast default", Summary: "将当前线程的 service tier 恢复为默认。"},
				{Command: "/fast toggle", Summary: "切换当前线程的响应速度设置。"},
				{Command: "/fast config", Summary: "打开当前线程的响应速度配置卡。"},
			},
		},
		{
			Names: []string{"/download"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
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
			Names: []string{"/review"},
			IsLocal: func(fields []string) bool {
				return matchReviewCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandReview(msg, args)
			},
			HelpGroup: "常用工具",
			HelpEntries: []helpCommandSpec{
				{Command: "/review", Summary: "审查当前 workspace 的未提交改动。"},
				{Command: "/review uncommitted", Summary: "等价于 `/review`。"},
				{Command: "/review base", Summary: "打开 base branch 选择卡。"},
				{Command: "/review base <branch>", Summary: "对比指定 branch 发起 review。"},
				{Command: "/review commit", Summary: "打开最近 100 个 commit 选择卡。"},
				{Command: "/review commit <rev>", Summary: "审查指定 commit/ref。"},
				{Command: "/review custom", Summary: "打开自定义 review instructions 卡。"},
				{Command: "/review custom <instructions>", Summary: "按自定义 instructions 发起 review。"},
			},
		},
		{
			Names: []string{"/compact"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
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
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
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
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
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
			IsLocal: func(fields []string) bool {
				return matchThreadCommand(fields)
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
				{Command: "/thread resume THREAD_ID", Summary: "按 thread id 直接恢复线程。"},
				{Command: "/thread sandbox", Summary: "配置当前线程的 sandbox。"},
				{Command: "/thread sandbox MODE", Summary: "直接设置当前线程的 sandbox。"},
				{Command: "/thread policy", Summary: "配置当前线程的 approval policy。"},
				{Command: "/thread policy POLICY", Summary: "直接设置当前线程的 approval policy。"},
			},
		},
		{
			Names: []string{"/threads"},
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
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
			IsLocal: func(fields []string) bool {
				return exactCommand(fields)
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
		},
		{
			Names: []string{"/codex"},
			IsLocal: func(fields []string) bool {
				return matchCodexCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandCodex(msg, args)
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
			Names: []string{"/upgrade"},
			IsLocal: func(fields []string) bool {
				return matchUpgradeCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandUpgrade(msg, args)
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
		{
			Names: []string{"/workspace"},
			IsLocal: func(fields []string) bool {
				return matchWorkspaceCommand(fields)
			},
			Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
				return a.commandWorkspace(msg, args)
			},
			HelpGroup: "workspace",
			HelpEntries: []helpCommandSpec{
				{Command: "/workspace", Summary: "打开工作区菜单。"},
				{Command: "/workspace list", Summary: "打开工作区列表并可直接切换。"},
				{Command: "/workspace new", Summary: "创建新工作区。"},
				{Command: "/workspace clone GIT_URL [ID] [--parent DIR]", Summary: "从 Git 仓库创建新工作区，可显式指定父目录。"},
				{Command: "/workspace use ID", Summary: "切换到指定工作区。"},
				{Command: "/workspace delete", Summary: "打开工作区删除菜单。"},
				{Command: "/workspace delete ID", Summary: "删除指定工作区的配置，不删除磁盘目录。"},
				{Command: "/workspace sandbox", Summary: "配置当前工作区默认 sandbox。"},
				{Command: "/workspace sandbox MODE", Summary: "直接设置当前工作区默认 sandbox。"},
				{Command: "/workspace policy", Summary: "配置当前工作区默认 approval policy。"},
				{Command: "/workspace policy POLICY", Summary: "直接设置当前工作区默认 approval policy。"},
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
