package features

import (
	"strings"

	"feidex/internal/domain/backend"
)

func FindMenuGroup(action string) (MenuGroupSpec, bool) {
	action = strings.TrimSpace(action)
	for _, spec := range MenuGroupSpecs() {
		if spec.Action == action {
			return spec, true
		}
	}
	return MenuGroupSpec{}, false
}
func FindMenuItem(action string) (MenuItemSpec, bool) {
	action = strings.TrimSpace(action)
	for _, spec := range MenuItemSpecs() {
		if spec.Action == action {
			return spec, true
		}
	}
	return MenuItemSpec{}, false
}

func MenuItemVisible(spec MenuItemSpec, kind string) bool {
	kind = backend.NormalizeBackend(kind)
	if kind == "" {
		return menuItemVisibleWithoutBackend(spec)
	}
	if spec.Kind == MenuItemBack {
		return true
	}
	if strings.TrimSpace(spec.Slash) == "" {
		return true
	}
	return HandlesCommand(kind, spec.Slash)
}

func menuItemVisibleWithoutBackend(spec MenuItemSpec) bool {
	if spec.Kind == MenuItemBack {
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

func MenuActionVisible(action, kind string) bool {
	action = strings.TrimSpace(action)
	kind = backend.NormalizeBackend(kind)
	if action == "" || action == "menu.root" {
		return true
	}
	if _, ok := FindMenuGroup(action); ok {
		return GroupHasVisibleMenuItems(action, kind)
	}
	if spec, ok := FindMenuItem(action); ok {
		return MenuItemVisible(spec, kind)
	}
	return true
}

func NearestVisibleMenuAction(action, kind string) string {
	action = strings.TrimSpace(action)
	kind = backend.NormalizeBackend(kind)
	if action == "" {
		action = "menu.root"
	}
	for i := 0; action != "" && i < 16; i++ {
		if MenuActionVisible(action, kind) {
			return action
		}
		node, ok := MenuNodes()[action]
		if !ok {
			break
		}
		action = strings.TrimSpace(node.Parent)
	}
	if MenuActionVisible("menu.root", kind) {
		return "menu.root"
	}
	return ""
}

func MenuItemsForGroup(action, kind string) []MenuItemSpec {
	action = strings.TrimSpace(action)
	items := make([]MenuItemSpec, 0, 8)
	for _, spec := range MenuItemSpecs() {
		if spec.GroupAction == action && MenuItemVisible(spec, kind) {
			items = append(items, spec)
		}
	}
	return items
}

func GroupHasVisibleMenuItems(action, kind string) bool {
	if backend.NormalizeBackend(kind) == "" {
		return menuActionVisibleWithoutBackend(action)
	}
	hasDeclaredItems := false
	for _, spec := range MenuItemSpecs() {
		if spec.GroupAction != strings.TrimSpace(action) {
			continue
		}
		hasDeclaredItems = true
		if spec.Kind == MenuItemBack {
			continue
		}
		if MenuItemVisible(spec, kind) {
			return true
		}
	}
	return !hasDeclaredItems
}

// RootMenuGroups returns the ordered groups that this frontend can display.
func RootMenuGroups(kind string) []MenuGroupSpec {
	groups := make([]MenuGroupSpec, 0, 8)
	for _, spec := range MenuGroupSpecs() {
		if spec.ShowInRoot && spec.Action != "menu.current_bot" && GroupHasVisibleMenuItems(spec.Action, kind) {
			groups = append(groups, spec)
		}
	}
	return groups
}
func MenuBackAction(action string) string {
	if node, ok := MenuNodes()[strings.TrimSpace(action)]; ok {
		if parent := strings.TrimSpace(node.Parent); parent != "" {
			return parent
		}
	}
	return "menu.root"
}
