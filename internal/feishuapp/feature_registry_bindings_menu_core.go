package feishuapp

import "feidex/internal/feishu"

func appendFeatureBindingsMenuCore(bindings map[string]featureBinding) {
	bindings["menu.root"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"menu": {
				Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
					return sendCommandMenu(a, msg)
				},
			},
		},
		RenderActions: []string{"menu.root"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.root" {
				return nil, false
			}
			return renderCommandMenuCardData(a.configView().configuredBackend(), planModeTitleForSession(a.State(), a != nil, sessionKey, "主菜单"), a.feishu, sessionKey), true
		},
		PortActions: []string{"menu.root"},
	}
	bindings["menu.tools"] = featureBinding{
		RenderActions: []string{"menu.tools"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.tools" {
				return nil, false
			}
			spec, _ := menuGroupSpec("menu.tools")
			return renderToolsMenuCardData(a.configView().configuredBackend(), planModeTitleForSession(a.State(), a != nil, sessionKey, spec.Label), a.feishu, sessionKey), true
		},
		PortActions: []string{"menu.tools"},
	}
	bindings["menu.group.model"] = featureBinding{
		RenderActions: []string{"menu.group.model"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.group.model" {
				return nil, false
			}
			return a.bindings.BackendConfiguration.RenderModelMenuCard(sessionKey), true
		},
		PortActions: []string{"menu.group.model"},
	}
	bindings["menu.group.system"] = featureBinding{
		RenderActions: []string{"menu.group.system"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.group.system" {
				return nil, false
			}
			spec, _ := menuGroupSpec("menu.group.system")
			return renderSystemMenuCardData(a.configView().configuredBackend(), planModeTitleForSession(a.State(), a != nil, sessionKey, spec.Label), a.feishu, sessionKey), true
		},
		PortActions: []string{"menu.group.system"},
	}
	bindings["menu.group.backend"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"backend": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.BackendSelection.CommandBackend(msg, args)
				},
			},
		},
		RenderActions: []string{"menu.group.backend"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.group.backend" {
				return nil, false
			}
			spec, _ := menuGroupSpec("menu.group.backend")
			return renderBackendMenuCardData(a.configView().configuredBackend(), planModeTitleForSession(a.State(), a != nil, sessionKey, spec.Label), a.feishu, sessionKey), true
		},
		PortActions: []string{"menu.group.backend", "menu.backend", "menu.backend.switch", "menu.auto_retry", "backend.select", "auto_retry.set"},
	}
}
