// Package menuutil provides menu navigation, breadcrumb, and button
// rendering helpers extracted from the app god package.
package menuutil

import (
	"strings"

	"feidex/internal/adapter/feishu/cards"
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

// PageCard assembles one menu page: the breadcrumb header, the page body, the
// renderer's forward controls, and the final 返回上一级 control, all derived
// from a single declared menu node. Renderers only supply the body and forward
// controls, so a page cannot forget its breadcrumb or misplace its back
// control. The node must be declared in the features registry; the menu graph
// guard test fails on undeclared breadcrumb paths, and an undeclared node
// falls back to the root menu path at runtime.
type PageCard struct {
	// Node is the declared menu node action this page claims.
	Node string
	// Backend selects backend-specific node labels; empty uses shared labels.
	Backend string
	// SessionKey is embedded in the back control. When empty, the back
	// control is omitted because it has no conversation to return through.
	SessionKey string
	// BackAction overrides the back target; empty uses the node's declared
	// parent via the features registry.
	BackAction string
	// BackParams are extra fields merged into the back control value, for
	// back targets that need context such as a page number.
	BackParams map[string]any
	Title      string
	Color      string
	Body       string
	// Buttons are the page's forward controls. The back control is appended
	// after them, keeping it the final interactive element.
	Buttons []feishu.Button
}

// Render assembles the page card.
func (p PageCard) Render() map[string]any {
	node := strings.TrimSpace(p.Node)
	if node == "" {
		node = "menu.root"
	}
	backAction := strings.TrimSpace(p.BackAction)
	if backAction == "" {
		backAction = menutypes.MenuBackAction(node)
	}
	backValue := map[string]any{
		"action":      backAction,
		"session_key": p.SessionKey,
	}
	for key, value := range p.BackParams {
		backValue[key] = value
	}
	body := MenuCardBodyForBackend(p.Backend, node, p.Body)
	buttons := append([]feishu.Button(nil), p.Buttons...)
	if strings.TrimSpace(p.SessionKey) != "" {
		buttons = append(buttons, feishu.Button{
			Text:  feishu.MenuBackButtonText,
			Type:  "default",
			Value: backValue,
		})
	}
	return feishu.SimpleStatusCard(p.Title, p.Color, body, buttons)
}

// MarkdownPageCard assembles one menu page built from markdown body elements
// (selects, forms, action rows): the declared breadcrumb header first, the
// page's own elements, the final 返回上一级 action row, and optional tail
// elements (apply-status notes) after it. Like PageCard, the back control is
// derived from the declared node so it cannot drift.
type MarkdownPageCard struct {
	Node       string
	Backend    string
	SessionKey string
	// BackAction overrides the back target; empty uses the node's declared
	// parent via the features registry.
	BackAction string
	Title      string
	Color      string
	// Body is optional leading markdown rendered together with the
	// breadcrumb; empty renders the breadcrumb line alone.
	Body string
	// Buttons are forward controls rendered as a single action row between the
	// page elements and the back control. A page that owns more than two
	// controls should lay them out through Elements with
	// cards.BuildMarkdownBodyCardActionElements instead, so each control keeps
	// its own row; sharing one row squeezes them past readability.
	Buttons []feishu.Button
	// Elements are the page's own elements between breadcrumb and back.
	Elements []map[string]any
	// Tail elements are appended after the back control (non-interactive
	// apply-status notes).
	Tail []map[string]any
}

// Render assembles the page card.
func (p MarkdownPageCard) Render() map[string]any {
	node := strings.TrimSpace(p.Node)
	if node == "" {
		node = "menu.root"
	}
	backAction := strings.TrimSpace(p.BackAction)
	if backAction == "" {
		backAction = menutypes.MenuBackAction(node)
	}
	card := cards.NewMarkdownBodyCard(p.Title, p.Color)
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": MenuCardBodyForBackend(p.Backend, node, p.Body)})
	for _, element := range p.Elements {
		cards.AppendMarkdownBodyCardElement(card, element)
	}
	if len(p.Buttons) > 0 {
		cards.AppendMarkdownBodyCardElement(card, cards.BuildMarkdownBodyCardActionElement(p.Buttons))
	}
	if strings.TrimSpace(p.SessionKey) != "" {
		cards.AppendMarkdownBodyCardElement(card, cards.BuildMarkdownBodyCardActionElement([]feishu.Button{{
			Text: feishu.MenuBackButtonText,
			Type: "default",
			Value: map[string]any{
				"action":      backAction,
				"session_key": p.SessionKey,
			},
		}}))
	}
	for _, element := range p.Tail {
		cards.AppendMarkdownBodyCardElement(card, element)
	}
	return card
}
