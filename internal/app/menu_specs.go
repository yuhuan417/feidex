package app

import (
	"strings"

	appmenuutil "feidex/internal/app/menuutil"
	"feidex/internal/app/menutypes"
	"feidex/internal/feishu"
)

func menuGroupSpec(action string) (commandMenuGroupSpec, bool) {
	return appmenuutil.MenuGroupSpec(action)
}

func menuGroupSpecForBackend(action, backend string) (commandMenuGroupSpec, bool) {
	return appmenuutil.MenuGroupSpecForBackend(action, backend)
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
	return appmenuutil.MenuItemSpecForAction(action)
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
	return appmenuutil.RenderRootMenuButtons(backend, sessionKey,
		func(spec menutypes.MenuItemSpec, backend string) bool {
			return menuItemVisibleForBackend(spec, backend)
		},
		func(action, backend string) bool {
			return groupHasVisibleMenuItems(action, backend)
		},
	)
}

func renderGroupMenuButtons(backend, groupAction, sessionKey string) []feishu.Button {
	return appmenuutil.RenderGroupMenuButtons(groupAction, sessionKey, func(action string) []commandMenuItemSpec {
		return menuItemsForGroup(action, backend)
	})
}

func renderMenuButtonSpec(spec commandMenuItemSpec, sessionKey string) feishu.Button {
	return appmenuutil.RenderMenuButtonSpec(spec, sessionKey)
}

func appendHelpCommands(lines []string, specs []helpCommandSpec) []string {
	return appmenuutil.AppendHelpCommands(lines, specs)
}
