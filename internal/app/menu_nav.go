package app

import (
	appmenuutil "feidex/internal/adapter/feishu/menuutil"
	menutypes "feidex/internal/application/features"
)

func menuBreadcrumbLabels(action string) []string {
	return appmenuutil.MenuBreadcrumbLabels(action)
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
func menuBackAction(action string) string { return menutypes.MenuBackAction(action) }

func commandLabel(label, slash string) string {
	return appmenuutil.CommandLabel(label, slash)
}

func submenuCommandLabel(label, slash string) string {
	return appmenuutil.SubmenuCommandLabel(label, slash)
}
