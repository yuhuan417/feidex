package app

import (
	"fmt"
	"strings"

	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func appendFeatureBindingsThreadWorkspace(bindings map[string]featureBinding) {
	bindings["menu.interrupt"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"interrupt": {
				Match: exactCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
					return appthreadmenu.NewService(a).CommandInterrupt(msg)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.interrupt" {
				return nil, nil
			}
			return appthreadmenu.NewService(s.app).CompleteMenuInterrupt(action, actionSessionKey(action), actionStringValue(action, "turn_id"))
		},
	}
	bindings["menu.thread"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"fork": {
				Match: exactCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandFork(a, msg, args)
				},
			},
			"new": {
				Match: exactCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
					return appthreadmenu.NewService(a).CommandThreadsNew(msg)
				},
			},
			"thread": {
				Match: matchThreadCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return appthreadmenu.NewService(a).CommandThread(msg, args)
				},
				Backends: map[string]func(fields []string) bool{
					backendClaude: nil,
				},
			},
			"session": {
				Match: matchSessionCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return appthreadmenu.NewService(a).CommandSession(msg, args)
				},
				Backends: map[string]func(fields []string) bool{
					backendCodex: nil,
				},
			},
			"threads": {
				Match: exactCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if len(args) > 0 {
						return fmt.Errorf("usage: /threads")
					}
					return appthreadmenu.NewService(a).CommandThread(msg, []string{"list"})
				},
				Backends: map[string]func(fields []string) bool{
					backendClaude: nil,
				},
			},
		},
		RenderActions: []string{"menu.thread"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.thread" {
				return nil, false
			}
			card, err := conversationBackend(a).RenderThreadsCard(sessionKey, false)
			if err != nil {
				return nil, false
			}
			return card, true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.thread":
				return appthreadmenu.NewService(s.app).CompleteMenuThread(action, sessionKey)
			case "menu.new":
				return appthreadmenu.NewService(s.app).CompleteMenuNew(action, sessionKey)
			case "menu.fork":
				return completeMenuFork(s.app, action, sessionKey)
			default:
				return nil, nil
			}
		},
	}
	bindings["menu.workspace"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"workspace": {
				Match: matchWorkspaceCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newWorkspaceService(a).commandWorkspace(msg, args)
				},
				Backends: map[string]func(fields []string) bool{
					backendClaude: matchClaudeWorkspaceCommand,
				},
			},
		},
		RenderActions: []string{"menu.workspace"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.workspace" {
				return nil, false
			}
			return newWorkspaceConfigService(a).renderWorkspaceMenuCard(sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.workspace" {
				return nil, nil
			}
			return completeMenuWorkspace(s.app, action, actionSessionKey(action))
		},
	}
	bindings["menu.model"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"model": {
				Match: matchModelCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newBackendConfigurationService(a).handleBackendModelCommand(msg, args)
				},
			},
			"effort": {
				Match: matchEffortCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return newModelConfigService(a).commandEffort(msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			switch actionName {
			case "menu.model":
				return newMenuActionService(s.app).completeMenuModel(action, actionSessionKey(action))
			case "model.config.set_model":
				return newBackendConfigurationService(s.app).completeGlobalModelSet(action, actionStringValue(action, "model_id"))
			case "model.config.select_model":
				modelID := strings.TrimSpace(action.Option)
				if modelID == modelConfigDefaultOptionValue {
					modelID = ""
				}
				return newBackendConfigurationService(s.app).completeGlobalModelSet(action, modelID)
			case "model.config.set_effort":
				return newBackendConfigurationService(s.app).completeGlobalReasoningEffortSet(action, actionStringValue(action, "reasoning_effort"))
			case "model.config.select_effort":
				reasoningEffort := strings.TrimSpace(action.Option)
				if reasoningEffort == modelConfigDefaultOptionValue {
					reasoningEffort = ""
				}
				return newBackendConfigurationService(s.app).completeGlobalReasoningEffortSet(action, reasoningEffort)
			default:
				return nil, nil
			}
		},
	}
	bindings["menu.fast"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"fast": {
				Match: func(fields []string) bool {
					return exactOrSingleArgCommand(fields, "config", "fast", "default", "off", "toggle")
				},
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandFast(a, msg, args)
				},
				Backends: map[string]func(fields []string) bool{
					backendClaude: nil,
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			switch actionName {
			case "menu.fast":
				return newMenuActionService(s.app).completeMenuFast(action, actionSessionKey(action))
			case "service_tier.set":
				return newMenuActionService(s.app).completeServiceTierSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "service_tier"))
			default:
				return nil, nil
			}
		},
	}
}
