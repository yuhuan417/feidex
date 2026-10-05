package feishuapp

import (
	"feidex/internal/feishu"
)

func appendFeatureBindingsBinding(bindings map[string]featureBinding) {
	bindings["menu.current_bot"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"primary": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.BindingCommands.commandPrimary(msg, args)
				},
			},
		},
		RenderActions: []string{"menu.current_bot"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.current_bot" {
				return nil, false
			}
			return renderCommandMenuCardData(a.configView().configuredBackend(), planModeTitleForSession(a.State(), a != nil, sessionKey, "主菜单"), a.feishu, sessionKey), true
		},
		PortActions: []string{"menu.current_bot"},
	}

	bindings["menu.current_workspace"] = featureBinding{
		RenderActions: []string{"menu.current_workspace"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.current_workspace" {
				return nil, false
			}
			return a.bindings.WorkspacePresentation.RenderWorkspaceMenuCard(sessionKey), true
		},
		PortActions: []string{"menu.current_workspace", "current_workspace.choose", "current_workspace.use"},
	}
}
