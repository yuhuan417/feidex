package app

import (
	"strings"

	"feidex/internal/feishu"
)

type commandMenuGroupSpec struct {
	Action      string
	Label       string
	Description string
}

type menuItemKind string

const (
	menuItemDirect  menuItemKind = "direct"
	menuItemSubmenu menuItemKind = "submenu"
	menuItemBack    menuItemKind = "back"
)

type commandMenuItemSpec struct {
	GroupAction         string
	Action              string
	Label               string
	Slash               string
	Kind                menuItemKind
	IncludeParentAction bool
}

type helpCommandSpec struct {
	Command string
	Summary string
}

type helpSectionSpec struct {
	Title    string
	Commands []helpCommandSpec
}

var commandMenuGroupSpecs = []commandMenuGroupSpec{
	{
		Action:      "menu.group.session",
		Label:       "会话行为",
		Description: "控制当前会话的行为与输出方式。",
	},
	{
		Action:      "menu.group.context",
		Label:       "会话管理",
		Description: "管理线程、上下文压缩与工作区上下文。",
	},
	{
		Action:      "menu.group.model",
		Label:       "模型能力",
		Description: "配置模型、推理强度与响应速度。",
	},
	{
		Action:      "menu.group.system",
		Label:       "服务管理",
		Description: "查看服务状态、执行升级，或查阅命令帮助。",
	},
}

var commandMenuItemSpecs = []commandMenuItemSpec{
	{GroupAction: "menu.group.session", Action: "menu.interrupt", Label: "中断任务", Slash: "/interrupt", Kind: menuItemDirect, IncludeParentAction: true},
	{GroupAction: "menu.group.session", Action: "menu.quiet", Label: "Quiet 模式", Slash: "/quiet", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.session", Action: "menu.usage", Label: "Token Usage", Slash: "/usage", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.session", Action: "menu.root", Label: "返回上一级", Kind: menuItemBack},

	{GroupAction: "menu.group.context", Action: "menu.new", Label: "新线程", Slash: "/new", Kind: menuItemDirect, IncludeParentAction: true},
	{GroupAction: "menu.group.context", Action: "menu.download", Label: "下载文件", Slash: "/download", Kind: menuItemDirect, IncludeParentAction: true},
	{GroupAction: "menu.group.context", Action: "menu.fork", Label: "Fork 线程", Slash: "/fork", Kind: menuItemDirect, IncludeParentAction: true},
	{GroupAction: "menu.group.context", Action: "menu.compact", Label: "压缩上下文", Slash: "/compact", Kind: menuItemDirect, IncludeParentAction: true},
	{GroupAction: "menu.group.context", Action: "menu.workspace", Label: "工作区管理", Slash: "/workspace", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.context", Action: "menu.threads", Label: "线程管理", Slash: "/threads", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.context", Action: "menu.root", Label: "返回上一级", Kind: menuItemBack},

	{GroupAction: "menu.group.model", Action: "menu.model", Label: "模型配置", Slash: "/model", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.model", Action: "menu.reasoning", Label: "推理强度", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.model", Action: "menu.fast", Label: "响应速度", Slash: "/fast", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.model", Action: "menu.root", Label: "返回上一级", Kind: menuItemBack},

	{GroupAction: "menu.group.system", Action: "menu.status", Label: "状态面板", Slash: "/status", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.system", Action: "menu.debug", Label: "调试日志", Slash: "/debug", Kind: menuItemDirect},
	{GroupAction: "menu.group.system", Action: "menu.debug.logs", Label: "查看日志", Slash: "/debug logs", Kind: menuItemDirect},
	{GroupAction: "menu.group.system", Action: "menu.upgrade", Label: "升级服务", Slash: "/upgrade", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.system", Action: "menu.help", Label: "帮助说明", Slash: "/help", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.system", Action: "menu.root", Label: "返回上一级", Kind: menuItemBack},
}

var helpIntroCommandSpecs = []helpCommandSpec{
	{Command: "/menu", Summary: "打开命令菜单。"},
	{Command: "/help", Summary: "查看所有本地命令与说明。"},
}

var helpSectionSpecs = []helpSectionSpec{
	{
		Title: "会话行为",
		Commands: []helpCommandSpec{
			{Command: "`/interrupt` 或 `/stop`", Summary: "中断当前运行中的任务，并清空排队/暂存输入。"},
			{Command: "/quiet", Summary: "切换 Quiet 模式。"},
			{Command: "/quiet on", Summary: "开启 Quiet 模式。"},
			{Command: "/quiet off", Summary: "关闭 Quiet 模式。"},
			{Command: "/debug", Summary: "切换服务端 slog 日志级别（debug/info）。"},
			{Command: "/debug on", Summary: "切换到 debug 级别。"},
			{Command: "/debug off", Summary: "切换到 info 级别。"},
			{Command: "/debug logs", Summary: "查看最近一段服务端 slog 日志。"},
		},
	},
	{
		Title: "会话管理",
		Commands: []helpCommandSpec{
			{Command: "/new", Summary: "切换到新线程模式，下一条消息会新建线程。"},
			{Command: "/download", Summary: "在当前 workspace 范围内选择文件，并生成下载链接。"},
			{Command: "/fork", Summary: "复制当前 thread 为一个新分支，并立即切换过去。"},
			{Command: "/compact", Summary: "压缩当前线程的上下文，减少上下文占用。"},
			{Command: "/threads", Summary: "查看当前工作区可恢复的线程。"},
			{Command: "/threads all", Summary: "查看更多来源的线程。"},
			{Command: "/threads fork", Summary: "等价于 `/fork`。"},
			{Command: "/threads new", Summary: "等价于 `/new`。"},
			{Command: "/threads sandbox", Summary: "配置当前线程的 sandbox。"},
			{Command: "/threads policy", Summary: "配置当前线程的 approval policy。"},
			{Command: "/history", Summary: "查看当前 thread 的 turn 历史记录，重点展示每个 turn 的输入。"},
			{Command: "/usage", Summary: "查看当前 thread 的累计 token usage。"},
			{Command: "`/workspace` 或 `/cd`", Summary: "打开工作区菜单。"},
			{Command: "/workspace list", Summary: "列出所有工作区。"},
			{Command: "/workspace new", Summary: "创建新工作区。"},
			{Command: "/workspace use ID", Summary: "切换到指定工作区。"},
			{Command: "/workspace sandbox", Summary: "配置当前工作区默认 sandbox。"},
			{Command: "/workspace policy", Summary: "配置当前工作区默认 approval policy。"},
		},
	},
	{
		Title: "模型能力",
		Commands: []helpCommandSpec{
			{Command: "/model", Summary: "打开模型与推理强度配置。"},
			{Command: "/fast", Summary: "切换当前线程的响应速度设置。"},
		},
	},
	{
		Title: "服务管理",
		Commands: []helpCommandSpec{
			{Command: "/status", Summary: "查看当前会话、线程、工作区与模型状态。"},
			{Command: "/upgrade [VERSION]", Summary: "检查最新版本，或直接升级到指定版本。"},
		},
	},
}

func menuGroupSpec(action string) (commandMenuGroupSpec, bool) {
	action = strings.TrimSpace(action)
	for _, spec := range commandMenuGroupSpecs {
		if spec.Action == action {
			return spec, true
		}
	}
	return commandMenuGroupSpec{}, false
}

func menuItemsForGroup(action string) []commandMenuItemSpec {
	action = strings.TrimSpace(action)
	items := make([]commandMenuItemSpec, 0, 8)
	for _, spec := range commandMenuItemSpecs {
		if spec.GroupAction == action {
			items = append(items, spec)
		}
	}
	return items
}

func renderRootMenuButtons(sessionKey string) []feishu.Button {
	buttons := make([]feishu.Button, 0, len(commandMenuGroupSpecs))
	for _, spec := range commandMenuGroupSpecs {
		buttons = append(buttons, feishu.Button{
			Text:  submenuLabel(spec.Label),
			Type:  "default",
			Value: map[string]any{"action": spec.Action, "session_key": sessionKey},
		})
	}
	return buttons
}

func renderGroupMenuButtons(groupAction, sessionKey string) []feishu.Button {
	items := menuItemsForGroup(groupAction)
	buttons := make([]feishu.Button, 0, len(items))
	for _, spec := range items {
		buttons = append(buttons, renderMenuButtonSpec(spec, sessionKey))
	}
	return buttons
}

func renderMenuButtonSpec(spec commandMenuItemSpec, sessionKey string) feishu.Button {
	text := spec.Label
	switch spec.Kind {
	case menuItemSubmenu:
		if strings.TrimSpace(spec.Slash) != "" {
			text = submenuCommandLabel(spec.Label, spec.Slash)
		} else {
			text = submenuLabel(spec.Label)
		}
	case menuItemDirect:
		text = commandLabel(spec.Label, spec.Slash)
	}
	value := map[string]any{"action": spec.Action, "session_key": sessionKey}
	if spec.IncludeParentAction {
		value["parent_action"] = spec.GroupAction
	}
	return feishu.Button{
		Text:  text,
		Type:  "default",
		Value: value,
	}
}

func renderHelpBodyFromSpecs() string {
	lines := []string{"命令说明：", ""}
	lines = appendHelpCommands(lines, helpIntroCommandSpecs)
	for _, section := range helpSectionSpecs {
		lines = append(lines, "", section.Title+"：")
		lines = appendHelpCommands(lines, section.Commands)
	}
	return strings.Join(lines, "\n")
}

func appendHelpCommands(lines []string, specs []helpCommandSpec) []string {
	for _, spec := range specs {
		command := spec.Command
		if !strings.Contains(command, "`") {
			command = "`" + command + "`"
		}
		lines = append(lines, command, spec.Summary)
	}
	return lines
}
