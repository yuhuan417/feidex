// Package menuutil provides menu navigation, breadcrumb, and button
// rendering helpers extracted from the app god package.
package menuutil

import (
	"strings"

	"feidex/internal/application/backendcaps"
	menutypes "feidex/internal/application/features"
	"feidex/internal/feishu"
)

// SubmenuLabel returns the submenu label with a trailing arrow.
func SubmenuLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "›"
	}
	return label + " ›"
}

// CommandLabel returns the command label with slash.
func CommandLabel(label, slash string) string {
	label = strings.TrimSpace(label)
	slash = strings.TrimSpace(slash)
	if label == "" {
		return slash
	}
	if slash == "" {
		return label
	}
	return label + " " + slash
}

// SubmenuCommandLabel returns the submenu command label.
func SubmenuCommandLabel(label, slash string) string {
	return SubmenuLabel(CommandLabel(label, slash))
}

// MenuBreadcrumbLabels returns breadcrumb labels for the given action.
func MenuBreadcrumbLabels(action string) []string {
	return MenuBreadcrumbLabelsForBackend(action, "")
}

// MenuBreadcrumbLabelsForBackend returns breadcrumb labels for the given action and backend.
func MenuBreadcrumbLabelsForBackend(action, backend string) []string {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "menu.root"
	}
	labels := []string{}
	for i := 0; action != "" && i < 16; i++ {
		node, ok := menutypes.MenuNodes()[action]
		if !ok {
			break
		}
		labels = append(labels, MenuNodeLabelForBackend(action, node.Label, backend))
		action = node.Parent
	}
	for i, j := 0, len(labels)-1; i < j; i, j = i+1, j-1 {
		labels[i], labels[j] = labels[j], labels[i]
	}
	return labels
}

// MenuCardBody returns the card body with a breadcrumb header.
func MenuCardBody(action, body string) string {
	return MenuCardBodyForBackend("", action, body)
}

// MenuCardBodyForBackend returns the card body with a breadcrumb header for the given backend.
func MenuCardBodyForBackend(backend, action, body string) string {
	breadcrumbs := strings.Join(MenuBreadcrumbLabelsForBackend(action, backend), " / ")
	body = strings.TrimSpace(body)
	if breadcrumbs == "" {
		return body
	}
	if body == "" {
		return "当前位置：" + breadcrumbs
	}
	return "当前位置：" + breadcrumbs + "\n\n" + body
}

// MenuNodeLabelForBackend returns the backend-specific label for a menu node.
func MenuNodeLabelForBackend(action, label, backend string) string {
	return backendcaps.ForKind(backend).MenuNodeLabel(action, label)
}

// MenuGroupSpec returns the menu group spec for the given action.
func MenuGroupSpec(action string) (menutypes.MenuGroupSpec, bool) {
	return MenuGroupSpecForBackend(action, "")
}

// MenuGroupSpecForBackend returns the menu group spec for the given action and backend.
func MenuGroupSpecForBackend(action, backend string) (menutypes.MenuGroupSpec, bool) {
	spec, ok := menutypes.FindMenuGroup(action)
	if !ok {
		return spec, false
	}
	return backendcaps.ForKind(backend).MenuGroupSpec(strings.TrimSpace(action), spec), true
}

// MenuItemSpecForAction returns the menu item spec for the given action.
func MenuItemSpecForAction(action string) (menutypes.MenuItemSpec, bool) {
	return menutypes.FindMenuItem(action)
}

// RenderRootMenuButtons consumes the application-selected group snapshot.
func RenderRootMenuButtons(backend, sessionKey string, groups []menutypes.MenuGroupSpec) []feishu.Button {
	buttons := make([]feishu.Button, 0, len(groups))
	for _, spec := range groups {
		spec = backendcaps.ForKind(backend).MenuGroupSpec(spec.Action, spec)
		buttons = append(buttons, feishu.Button{Text: SubmenuLabel(spec.Label), Type: "default", Value: map[string]any{"action": spec.Action, "session_key": sessionKey}})
	}
	return buttons
}

// RenderGroupMenuButtons consumes selected items without querying policy.
func RenderGroupMenuButtons(sessionKey string, items []menutypes.MenuItemSpec) []feishu.Button {
	buttons := make([]feishu.Button, 0, len(items))
	backButtons := make([]feishu.Button, 0, 1)
	for _, spec := range items {
		button := RenderMenuButtonSpec(spec, sessionKey)
		if spec.Kind == menutypes.MenuItemBack {
			backButtons = append(backButtons, button)
			continue
		}
		buttons = append(buttons, button)
	}
	return append(buttons, backButtons...)
}

// RenderMenuButtonSpec renders a single menu button from a spec.
func RenderMenuButtonSpec(spec menutypes.MenuItemSpec, sessionKey string) feishu.Button {
	text := spec.Label
	switch spec.Kind {
	case menutypes.MenuItemSubmenu:
		if strings.TrimSpace(spec.Slash) != "" {
			text = SubmenuCommandLabel(spec.Label, spec.Slash)
		} else {
			text = SubmenuLabel(spec.Label)
		}
	case menutypes.MenuItemDirect:
		text = CommandLabel(spec.Label, spec.Slash)
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

// AppendHelpCommands appends help command lines to the given lines slice.
func AppendHelpCommands(lines []string, specs []menutypes.HelpCommandSpec) []string {
	for _, spec := range specs {
		command := spec.Command
		if !strings.Contains(command, "`") {
			command = "`" + command + "`"
		}
		lines = append(lines, command, spec.Summary)
	}
	return lines
}
