package app

import (
	"strings"

	"feidex/internal/feishu"
)

type commandMenuGroupSpec struct {
	Action      string
	Label       string
	Description string
	ShowInRoot  bool
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

var commandMenuGroupSpecs = []commandMenuGroupSpec{
	{
		Action:      "menu.tools",
		Label:       "常用工具",
		Description: "常用会话与线程工具入口。",
		ShowInRoot:  true,
	},
	{
		Action:      "menu.group.model",
		Label:       "模型配置",
		Description: "模型选择与响应速度配置。",
		ShowInRoot:  true,
	},
	{
		Action:      "menu.thread",
		Label:       "线程管理",
		Description: "查看当前线程状态，并通过下拉切换线程。",
		ShowInRoot:  true,
	},
	{
		Action:      "menu.workspace",
		Label:       "工作区管理",
		Description: "查看当前工作区状态，并通过下拉切换工作区。",
		ShowInRoot:  true,
	},
	{
		Action:      "menu.group.system",
		Label:       "系统运维",
		Description: "系统运维与帮助入口。",
		ShowInRoot:  true,
	},
	{
		Action:      "menu.group.backend",
		Label:       "后端选择",
		Description: "后端切换与各 CLI 管理入口。",
		ShowInRoot:  false,
	},
}

var commandMenuItemSpecs = []commandMenuItemSpec{
	{GroupAction: "menu.tools", Action: "menu.interrupt", Label: "中断任务", Slash: "/stop", Kind: menuItemDirect, IncludeParentAction: true},
	{GroupAction: "menu.tools", Action: "menu.review", Label: "代码审查", Slash: "/review", Kind: menuItemSubmenu},
	{GroupAction: "menu.tools", Action: "menu.quiet", Label: "静默模式", Slash: "/quiet config", Kind: menuItemSubmenu},
	{GroupAction: "menu.tools", Action: "menu.compact", Label: "压缩上下文", Slash: "/compact", Kind: menuItemDirect, IncludeParentAction: true},
	{GroupAction: "menu.tools", Action: "menu.download", Label: "下载文件", Slash: "/download", Kind: menuItemDirect, IncludeParentAction: true},
	{GroupAction: "menu.tools", Action: "menu.history", Label: "历史记录", Slash: "/history", Kind: menuItemSubmenu},
	{GroupAction: "menu.tools", Action: "menu.skills", Label: "技能列表", Slash: "/skills", Kind: menuItemSubmenu},
	{GroupAction: "menu.tools", Action: "menu.usage", Label: "Token 消耗", Slash: "/usage", Kind: menuItemSubmenu},
	{GroupAction: "menu.tools", Action: "menu.root", Label: "返回上一级", Kind: menuItemBack},

	{GroupAction: "menu.group.model", Action: "menu.model", Label: "模型配置", Slash: "/model", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.model", Action: "menu.fast", Label: "响应速度", Slash: "/fast config", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.model", Action: "menu.root", Label: "返回上一级", Kind: menuItemBack},

	{GroupAction: "menu.group.system", Action: "menu.debug", Label: "日志级别", Slash: "/debug", Kind: menuItemDirect},
	{GroupAction: "menu.group.system", Action: "menu.debug.logs", Label: "查看日志", Slash: "/debug logs", Kind: menuItemDirect},
	{GroupAction: "menu.group.system", Action: "menu.group.backend", Label: "后端选择", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.system", Action: "menu.upgrade", Label: "升级服务", Slash: "/upgrade", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.system", Action: "menu.status", Label: "状态面板", Slash: "/status", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.system", Action: "menu.help", Label: "命令帮助", Slash: "/help", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.system", Action: "menu.root", Label: "返回上一级", Kind: menuItemBack},

	{GroupAction: "menu.group.backend", Action: "menu.backend.switch", Label: "切换后端", Slash: "/backend", Kind: menuItemDirect},
	{GroupAction: "menu.group.backend", Action: "menu.codex_upgrade", Label: "Codex 管理", Slash: "/codex", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.backend", Action: "menu.claude_upgrade", Label: "Claude 管理", Slash: "/claude", Kind: menuItemSubmenu},
	{GroupAction: "menu.group.backend", Action: "menu.group.system", Label: "返回上一级", Kind: menuItemBack},
}

func menuGroupSpec(action string) (commandMenuGroupSpec, bool) {
	return menuGroupSpecForBackend(action, "")
}

func menuGroupSpecForBackend(action, backend string) (commandMenuGroupSpec, bool) {
	action = strings.TrimSpace(action)
	for _, spec := range commandMenuGroupSpecs {
		if spec.Action == action {
			return backendCapabilityForKind(backend).menuGroupSpec(action, spec), true
		}
	}
	return commandMenuGroupSpec{}, false
}

func menuItemVisibleForBackend(spec commandMenuItemSpec, backend string) bool {
	backend = firstNonEmpty(normalizeRuntimeBackend(backend), backendCodex)
	if spec.Kind == menuItemBack {
		return true
	}
	if strings.TrimSpace(spec.Slash) == "" {
		return true
	}
	return isLocalCommandForBackend(backend, spec.Slash)
}

func menuItemSpecForAction(action string) (commandMenuItemSpec, bool) {
	action = strings.TrimSpace(action)
	for _, spec := range commandMenuItemSpecs {
		if spec.Action == action {
			return spec, true
		}
	}
	return commandMenuItemSpec{}, false
}

func menuActionVisibleForBackend(action, backend string) bool {
	action = strings.TrimSpace(action)
	backend = firstNonEmpty(normalizeRuntimeBackend(backend), backendCodex)
	if action == "" || action == "menu.root" {
		return true
	}
	if _, ok := menuGroupSpec(action); ok {
		return groupHasVisibleMenuItems(action, backend)
	}
	if spec, ok := menuItemSpecForAction(action); ok {
		return menuItemVisibleForBackend(spec, backend)
	}
	return true
}

func nearestVisibleMenuAction(action, backend string) string {
	action = strings.TrimSpace(action)
	backend = firstNonEmpty(normalizeRuntimeBackend(backend), backendCodex)
	if action == "" {
		action = "menu.root"
	}
	for i := 0; action != "" && i < 16; i++ {
		if menuActionVisibleForBackend(action, backend) {
			return action
		}
		node, ok := menuNodes[action]
		if !ok {
			break
		}
		action = strings.TrimSpace(node.Parent)
	}
	if menuActionVisibleForBackend("menu.root", backend) {
		return "menu.root"
	}
	return ""
}

func menuItemsForGroup(action, backend string) []commandMenuItemSpec {
	action = strings.TrimSpace(action)
	items := make([]commandMenuItemSpec, 0, 8)
	for _, spec := range commandMenuItemSpecs {
		if spec.GroupAction == action && menuItemVisibleForBackend(spec, backend) {
			items = append(items, spec)
		}
	}
	return items
}

func groupHasVisibleMenuItems(action, backend string) bool {
	hasDeclaredItems := false
	for _, spec := range commandMenuItemSpecs {
		if spec.GroupAction != strings.TrimSpace(action) {
			continue
		}
		hasDeclaredItems = true
		if spec.Kind == menuItemBack {
			continue
		}
		if menuItemVisibleForBackend(spec, backend) {
			return true
		}
	}
	return !hasDeclaredItems
}

func renderRootMenuButtons(backend, sessionKey string) []feishu.Button {
	buttons := make([]feishu.Button, 0, len(commandMenuGroupSpecs))
	for _, spec := range commandMenuGroupSpecs {
		if !spec.ShowInRoot {
			continue
		}
		if !groupHasVisibleMenuItems(spec.Action, backend) {
			continue
		}
		spec, _ = menuGroupSpecForBackend(spec.Action, backend)
		buttons = append(buttons, feishu.Button{
			Text:  submenuLabel(spec.Label),
			Type:  "default",
			Value: map[string]any{"action": spec.Action, "session_key": sessionKey},
		})
	}
	return buttons
}

func renderGroupMenuButtons(backend, groupAction, sessionKey string) []feishu.Button {
	items := menuItemsForGroup(groupAction, backend)
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
