// Package menuutil provides menu navigation, breadcrumb, and button
// rendering helpers extracted from the app god package.
package menuutil

import (
	"strings"

	"feidex/internal/app/backendcaps"
	"feidex/internal/app/menutypes"
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
		node, ok := menutypes.MenuNodes[action]
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
	action = strings.TrimSpace(action)
	for _, spec := range menutypes.MenuGroupSpecs {
		if spec.Action == action {
			return backendcaps.ForKind(backend).MenuGroupSpec(action, spec), true
		}
	}
	return menutypes.MenuGroupSpec{}, false
}

// MenuItemSpecForAction returns the menu item spec for the given action.
func MenuItemSpecForAction(action string) (menutypes.MenuItemSpec, bool) {
	action = strings.TrimSpace(action)
	for _, spec := range menutypes.MenuItemSpecs {
		if spec.Action == action {
			return spec, true
		}
	}
	return menutypes.MenuItemSpec{}, false
}

// RenderRootMenuButtons renders the top-level menu buttons.
// The isItemVisible callback determines whether a menu item is visible for the backend.
func RenderRootMenuButtons(backend, sessionKey string, isItemVisible func(spec menutypes.MenuItemSpec, backend string) bool, groupHasItems func(action, backend string) bool) []feishu.Button {
	visible := make([]menutypes.MenuGroupSpec, 0, len(menutypes.MenuGroupSpecs))
	for _, spec := range menutypes.MenuGroupSpecs {
		if !spec.ShowInRoot {
			continue
		}
		if groupHasItems != nil && !groupHasItems(spec.Action, backend) {
			continue
		}
		spec, _ = MenuGroupSpecForBackend(spec.Action, backend)
		visible = append(visible, spec)
	}
	buttons := make([]feishu.Button, 0, len(visible))
	for _, spec := range visible {
		buttons = append(buttons, feishu.Button{
			Text:  SubmenuLabel(spec.Label),
			Type:  "default",
			Value: map[string]any{"action": spec.Action, "session_key": sessionKey},
		})
	}
	return buttons
}

// RenderGroupMenuButtons renders menu buttons for a group.
// The getItems callback returns visible items for the group.
func RenderGroupMenuButtons(groupAction, sessionKey string, getItems func(action string) []menutypes.MenuItemSpec) []feishu.Button {
	items := getItems(groupAction)
	buttons := make([]feishu.Button, 0, len(items))
	for _, spec := range items {
		buttons = append(buttons, RenderMenuButtonSpec(spec, sessionKey))
	}
	return buttons
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

// HelpGroupOrder defines the order of help groups in the help output.
var HelpGroupOrder = []string{
	"常用工具",
	"model",
	"thread",
	"workspace",
	"system",
}
