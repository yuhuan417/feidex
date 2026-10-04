package feishuapp

func renderCommandMenuCard(a *App, sessionKey string) map[string]any {
	return renderCommandMenuCardData(
		a.configView().configuredBackend(), planModeTitleForSession(a, sessionKey, "主菜单"), a.feishu, sessionKey,
	)
}

func renderToolsMenuCard(a *App, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.tools")
	return renderToolsMenuCardData(
		a.configView().configuredBackend(), planModeTitleForSession(a, sessionKey, spec.Label), a.feishu, sessionKey,
	)
}

func renderSystemMenuCard(a *App, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.system")
	return renderSystemMenuCardData(
		a.configView().configuredBackend(), planModeTitleForSession(a, sessionKey, spec.Label), a.feishu, sessionKey,
	)
}

func renderBackendMenuCard(a *App, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.backend")
	return renderBackendMenuCardData(
		a.configView().configuredBackend(), planModeTitleForSession(a, sessionKey, spec.Label), a.feishu, sessionKey,
	)
}

func renderHelpCard(a *App, sessionKey string) map[string]any {
	return renderHelpCardData(
		a.configView().configuredBackend(), planModeTitleForSession(a, sessionKey, "帮助说明"),
		a.feishu, a.bindings.BindingCommands.scope, sessionKey,
	)
}
