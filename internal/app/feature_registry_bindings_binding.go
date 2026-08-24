package app

import (
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func appendFeatureBindingsBinding(bindings map[string]featureBinding) {
	bindings["menu.current_bot"] = featureBinding{
		RenderActions: []string{"menu.current_bot"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.current_bot" {
				return nil, false
			}
			return renderCurrentBotMenu(a, sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.current_bot" {
				return nil, nil
			}
			sessionKey := actionSessionKey(action)
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "info", Content: "已打开当前 Bot"},
				Card:  rawCard(renderCurrentBotMenu(s.app, sessionKey)),
			}, nil
		},
	}

	bindings["menu.binding"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"bind": {
				Match: func(fields []string) bool {
					return len(fields) > 0 && strings.TrimSpace(fields[0]) == "/bind"
				},
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newBindingService(a).commandBind(msg, args)
				},
			},
		},
		RenderActions: []string{"menu.binding"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.binding" {
				return nil, false
			}
			chatType, chatID, _, _ := parseSessionKeyMeta(sessionKey)
			return newBindingService(a).renderBindingStatusCard(sessionKey, agentBindingForChat(a, chatType, chatID)), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.binding":
				return newBindingService(s.app).completeMenuBinding(action, sessionKey)
			case "bind.choose":
				return newBindingService(s.app).completeBindingWorkspaceChoose(action, sessionKey)
			case "bind.use":
				return newBindingService(s.app).completeBindingUse(action, sessionKey, actionStringValue(action, "workspace_id"))
			default:
				return nil, nil
			}
		},
	}
}
