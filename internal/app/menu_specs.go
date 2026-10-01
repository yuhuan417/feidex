package app

import (
	"strings"

	"feidex/internal/app/menutypes"
	appmenuutil "feidex/internal/app/menuutil"
	"feidex/internal/feishu"
)

func menuGroupSpec(action string) (menutypes.MenuGroupSpec, bool) {
	return appmenuutil.MenuGroupSpec(action)
}

func menuItemVisibleForBackend(spec menutypes.MenuItemSpec, backend string) bool {
	backend = normalizeRuntimeBackend(backend)
	if backend == "" {
		return menuItemVisibleWithoutBackend(spec)
	}
	if spec.Kind == menutypes.MenuItemBack {
		return true
	}
	if strings.TrimSpace(spec.Slash) == "" {
		return true
	}
	return isLocalCommandForBackend(backend, spec.Slash)
}

func menuItemVisibleWithoutBackend(spec menutypes.MenuItemSpec) bool {
	if spec.Kind == menutypes.MenuItemBack {
		return true
	}
	switch strings.TrimSpace(spec.Action) {
	case "menu.group.backend", "menu.backend.switch", "menu.help":
		return true
	default:
		return false
	}
}

func menuActionVisibleWithoutBackend(action string) bool {
	switch strings.TrimSpace(action) {
	case "", "menu.root", "menu.group.system", "menu.group.backend", "menu.backend.switch", "menu.help":
		return true
	default:
		return false
	}
}

func menuItemSpecForAction(action string) (menutypes.MenuItemSpec, bool) {
	return appmenuutil.MenuItemSpecForAction(action)
}

func menuActionVisibleForBackend(action, backend string) bool {
	action = strings.TrimSpace(action)
	backend = normalizeRuntimeBackend(backend)
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
	backend = normalizeRuntimeBackend(backend)
	if action == "" {
		action = "menu.root"
	}
	for i := 0; action != "" && i < 16; i++ {
		if menuActionVisibleForBackend(action, backend) {
			return action
		}
		node, ok := menutypes.MenuNodes[action]
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

func menuItemsForGroup(action, backend string) []menutypes.MenuItemSpec {
	action = strings.TrimSpace(action)
	items := make([]menutypes.MenuItemSpec, 0, 8)
	for _, spec := range menutypes.MenuItemSpecs {
		if spec.GroupAction == action && menuItemVisibleForBackend(spec, backend) {
			items = append(items, spec)
		}
	}
	return items
}

func groupHasVisibleMenuItems(action, backend string) bool {
	if normalizeRuntimeBackend(backend) == "" {
		return menuActionVisibleWithoutBackend(action)
	}
	hasDeclaredItems := false
	for _, spec := range menutypes.MenuItemSpecs {
		if spec.GroupAction != strings.TrimSpace(action) {
			continue
		}
		hasDeclaredItems = true
		if spec.Kind == menutypes.MenuItemBack {
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
			if !menuGroupVisibleForSession(action, sessionKey) {
				return false
			}
			return groupHasVisibleMenuItems(action, backend)
		},
	)
}

func menuGroupVisibleForSession(action, sessionKey string) bool {
	switch strings.TrimSpace(action) {
	case "menu.current_bot":
		return false
	default:
		return true
	}
}

func renderGroupMenuButtons(backend, groupAction, sessionKey string) []feishu.Button {
	return appmenuutil.RenderGroupMenuButtons(groupAction, sessionKey, func(action string) []menutypes.MenuItemSpec {
		return menuItemsForGroup(action, backend)
	})
}

func appendHelpCommands(lines []string, specs []menutypes.HelpCommandSpec) []string {
	return appmenuutil.AppendHelpCommands(lines, specs)
}
