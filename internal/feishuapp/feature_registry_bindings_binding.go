package feishuapp

import (
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
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
			return renderCommandMenuCardData(a.configView().configuredBackend(), planModeTitleForSession(a, sessionKey, "主菜单"), a.feishu, sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.current_bot" {
				return nil, nil
			}
			sessionKey := actionSessionKey(action)
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "info", Content: "已返回命令菜单"},
				Card:  rawCard(renderCommandMenuCardData(s.app.configView().configuredBackend(), planModeTitleForSession(s.app, sessionKey, "主菜单"), s.app.feishu, sessionKey)),
			}, nil
		},
	}

	bindings["menu.current_workspace"] = featureBinding{
		RenderActions: []string{"menu.current_workspace"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.current_workspace" {
				return nil, false
			}
			return a.bindings.WorkspacePresentation.RenderWorkspaceMenuCard(sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.current_workspace":
				return completeMenuCommand(s.app, action, sessionKey, "/workspace", "menu.root")
			case "current_workspace.choose":
				return completeMenuCommand(s.app, action, sessionKey, "/workspace choose", "menu.workspace")
			case "current_workspace.use":
				return s.app.bindings.BindingCommands.completeBindingUse(action, sessionKey, actionStringValue(action, "workspace_id"))
			default:
				return nil, nil
			}
		},
	}
}
