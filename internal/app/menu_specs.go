package app

import (
	"strings"

	"feidex/internal/feishu"
)

func menuGroupSpec(action string) (commandMenuGroupSpec, bool) {
	return menuGroupSpecForBackend(action, "")
}

func menuGroupSpecForBackend(action, backend string) (commandMenuGroupSpec, bool) {
	action = strings.TrimSpace(action)
	for _, spec := range commandMenuGroupSpecs {
		if spec.Action == action {
			return backendCapabilityForKind(backend).MenuGroupSpec(action, spec), true
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
