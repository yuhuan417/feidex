package app

import (
	"strings"

	appmenuutil "feidex/internal/app/menuutil"
)

func menuBreadcrumbLabels(action string) []string {
	return appmenuutil.MenuBreadcrumbLabels(action)
}

func menuBreadcrumbLabelsForBackend(action, backend string) []string {
	return appmenuutil.MenuBreadcrumbLabelsForBackend(action, backend)
}

func menuCardBody(action, body string) string {
	return appmenuutil.MenuCardBody(action, body)
}

func menuCardBodyForBackend(backend, action, body string) string {
	return appmenuutil.MenuCardBodyForBackend(backend, action, body)
}

func menuCardBodyForSession(a *App, sessionKey, action, body string) string {
	return menuCardBody(action, body)
}

func menuCardBodyForBackendForSession(a *App, sessionKey, backend, action, body string) string {
	return menuCardBodyForBackend(backend, action, body)
}

// menuBackAction returns the action a card rendered for the given menu action
// returns to when the user taps the back control: the node's parent, falling
// back to the root menu.
func menuBackAction(action string) string {
	action = strings.TrimSpace(action)
	if action != "" {
		if node, ok := menuNodes[action]; ok {
			if parent := strings.TrimSpace(node.Parent); parent != "" {
				return parent
			}
		}
	}
	return "menu.root"
}

func menuNodeLabelForBackend(action, label, backend string) string {
	return appmenuutil.MenuNodeLabelForBackend(action, label, backend)
}

func submenuLabel(label string) string {
	return appmenuutil.SubmenuLabel(label)
}

func commandLabel(label, slash string) string {
	return appmenuutil.CommandLabel(label, slash)
}

func submenuCommandLabel(label, slash string) string {
	return appmenuutil.SubmenuCommandLabel(label, slash)
}
