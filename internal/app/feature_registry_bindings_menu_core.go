package app

import (
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func appendFeatureBindingsMenuCore(bindings map[string]featureBinding) {
	bindings["menu.root"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"menu": {
				Match: exactCommand,
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
			return renderCommandMenuCard(a, sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.root" {
				return nil, nil
			}
			return newMenuActionService(s.app).completeMenuRoot(action, actionSessionKey(action))
		},
	}
	bindings["menu.tools"] = featureBinding{
		RenderActions: []string{"menu.tools"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.tools" {
				return nil, false
			}
			return renderToolsMenuCard(a, sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.tools" {
				return nil, nil
			}
			return newMenuActionService(s.app).completeMenuTools(action, actionSessionKey(action))
		},
	}
	bindings["menu.group.model"] = featureBinding{
		RenderActions: []string{"menu.group.model"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.group.model" {
				return nil, false
			}
			return newBackendConfigurationService(a).renderModelMenuCard(sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.group.model" {
				return nil, nil
			}
			return newMenuActionService(s.app).completeMenuGroupModel(action, actionSessionKey(action))
		},
	}
	bindings["menu.group.system"] = featureBinding{
		RenderActions: []string{"menu.group.system"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.group.system" {
				return nil, false
			}
			return renderSystemMenuCard(a, sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.group.system" {
				return nil, nil
			}
			return newMenuActionService(s.app).completeMenuGroupSystem(action, actionSessionKey(action))
		},
	}
	bindings["menu.group.backend"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"backend": {
				Match: matchBackendCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newBackendSelectionService(a).commandBackend(msg, args)
				},
			},
		},
		RenderActions: []string{"menu.group.backend"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.group.backend" {
				return nil, false
			}
			return renderBackendMenuCard(a, sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.group.backend":
				return newBackendSelectionService(s.app).completeMenuBackend(action, sessionKey)
			case "menu.backend", "menu.backend.switch":
				return newMenuActionService(s.app).completeMenuBackendSwitch(action, sessionKey)
			case "menu.auto_retry":
				return completeMenuCommand(s.app, action, sessionKey, "/backend retry", "menu.group.backend")
			case "backend.select":
				return newBackendSelectionService(s.app).completeBackendSelect(action, sessionKey, actionStringValue(action, "backend"))
			case "auto_retry.set":
				return newAutoRetryService(s.app).CompleteAutoRetrySet(action, strings.EqualFold(actionStringValue(action, "enabled"), "on"))
			default:
				return nil, nil
			}
		},
	}
}
