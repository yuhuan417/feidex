package feishuapp

func renderCommandMenuCard(a *Frontend, sessionKey string) map[string]any {
	return renderCommandMenuCardData(
		a.configView().configuredBackend(), planModeTitleForSession(a.State(), a != nil, sessionKey, "主菜单"), a.feishu, sessionKey,
	)
}

func renderToolsMenuCard(a *Frontend, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.tools")
	return renderToolsMenuCardData(
		a.configView().configuredBackend(), planModeTitleForSession(a.State(), a != nil, sessionKey, spec.Label), a.feishu, sessionKey,
	)
}

func renderSystemMenuCard(a *Frontend, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.system")
	return renderSystemMenuCardData(
		a.configView().configuredBackend(), planModeTitleForSession(a.State(), a != nil, sessionKey, spec.Label), a.feishu, sessionKey,
	)
}

func renderBackendMenuCard(a *Frontend, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.backend")
	return renderBackendMenuCardData(
		a.configView().configuredBackend(), planModeTitleForSession(a.State(), a != nil, sessionKey, spec.Label), a.feishu, sessionKey,
	)
}

func renderHelpCard(a *Frontend, sessionKey string) map[string]any {
	return renderHelpCardData(
		a.configView().configuredBackend(), planModeTitleForSession(a.State(), a != nil, sessionKey, "帮助说明"),
		a.bindings.BindingCommands.scope, sessionKey,
	)
}
