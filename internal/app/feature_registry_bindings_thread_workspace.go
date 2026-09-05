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
			sessionKey = threadMenuEffectiveSessionKey(a, sessionKey)
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
					if groupBindingScopeActive(a, msg) {
						return newBindingService(a).commandWorkspace(msg, args)
					}
					return commandWorkspaceProfileAware(a, msg, args)
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
			return newWorkspaceRenderServiceInner(a).RenderWorkspaceMenuCard(sessionKey), true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.workspace" {
				return nil, nil
			}
			return completeMenuCommand(s.app, action, actionSessionKey(action), "/workspace", "menu.root")
		},
	}
	bindings["menu.model"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"model": {
				Match: matchModelCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if groupBindingScopeActive(a, msg) {
						return newBindingService(a).commandModel(msg, args)
					}
					return commandModelProfileAware(a, msg, args)
				},
				Backends: map[string]func(fields []string) bool{
					backendClaude: func(fields []string) bool {
						return matchModelCommand(fields) && !(len(fields) >= 2 && strings.TrimSpace(fields[1]) == "plan")
					},
				},
			},
			"effort": {
				Match: matchEffortCommand,
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if groupBindingScopeActive(a, msg) {
						return newBindingService(a).commandEffort(msg, args)
					}
					return commandEffortProfileAware(a, msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			if groupBindingSessionScopeActive(s.app, sessionKey) {
				svc := newBindingService(s.app)
				switch actionName {
				case "menu.model":
					msg := commandMessageFromAction(s.app, action, sessionKey, "/model")
					binding, err := svc.ensureBindingForMessage(msg)
					if err != nil {
						return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
					}
					card, err := svc.renderBindingModelConfigCard(sessionKey, binding)
					if err != nil {
						return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
					}
					return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已打开当前群内模型配置"}, Card: rawCard(card)}, nil
				case "model.config.set_model":
					return svc.completeBindingModelSet(action, sessionKey, actionStringValue(action, "model_id"))
				case "model.config.select_model":
					modelID := strings.TrimSpace(action.Option)
					if modelID == modelConfigDefaultOptionValue {
						modelID = ""
					}
					return svc.completeBindingModelSet(action, sessionKey, modelID)
				case "model.config.set_effort":
					return svc.completeBindingEffortSet(action, sessionKey, actionStringValue(action, "reasoning_effort"))
				case "model.config.select_effort":
					reasoningEffort := strings.TrimSpace(action.Option)
					if reasoningEffort == modelConfigDefaultOptionValue {
						reasoningEffort = ""
					}
					return svc.completeBindingEffortSet(action, sessionKey, reasoningEffort)
				case "model.config.add_option", "model.config.remove_option", "model.plan_config.set_model", "model.plan_config.select_model", "model.plan_config.set_effort", "model.plan_config.select_effort":
					return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "该项是 bot frontend 默认配置，请私聊该 bot 使用"}}, nil
				}
			}
			if p2pSessionScopeActive(s.app, sessionKey) {
				switch actionName {
				case "model.config.set_model":
					return completeBotProfileModelSet(s.app, action, actionStringValue(action, "model_id"))
				case "model.config.select_model":
					modelID := strings.TrimSpace(action.Option)
					if modelID == modelConfigDefaultOptionValue {
						modelID = ""
					}
					return completeBotProfileModelSet(s.app, action, modelID)
				case "model.config.set_effort":
					return completeBotProfileEffortSet(s.app, action, actionStringValue(action, "reasoning_effort"))
				case "model.config.select_effort":
					effort := strings.TrimSpace(action.Option)
					if effort == modelConfigDefaultOptionValue {
						effort = ""
					}
					return completeBotProfileEffortSet(s.app, action, effort)
				}
			}
			switch actionName {
			case "menu.model":
				return newMenuActionService(s.app).completeMenuModel(action, sessionKey)
			case "model.config.set_model":
				return newBackendConfigurationService(s.app).completeGlobalModelSet(action, actionStringValue(action, "model_id"))
			case "model.config.select_model":
				modelID := strings.TrimSpace(action.Option)
				if modelID == modelConfigDefaultOptionValue {
					modelID = ""
				}
				return newBackendConfigurationService(s.app).completeGlobalModelSet(action, modelID)
			case "model.config.add_option":
				return newBackendConfigurationService(s.app).completeClaudeModelOptionAdd(action)
			case "model.config.remove_option":
				return newBackendConfigurationService(s.app).completeClaudeModelOptionRemove(action)
			case "model.config.set_effort":
				return newBackendConfigurationService(s.app).completeGlobalReasoningEffortSet(action, actionStringValue(action, "reasoning_effort"))
			case "model.config.select_effort":
				reasoningEffort := strings.TrimSpace(action.Option)
				if reasoningEffort == modelConfigDefaultOptionValue {
					reasoningEffort = ""
				}
				return newBackendConfigurationService(s.app).completeGlobalReasoningEffortSet(action, reasoningEffort)
			case "model.plan_config.set_model":
				return newModelConfigService(s.app).completeCodexPlanModelSet(action, actionStringValue(action, "model_id"))
			case "model.plan_config.select_model":
				modelID := strings.TrimSpace(action.Option)
				if modelID == modelConfigDefaultOptionValue {
					modelID = ""
				}
				return newModelConfigService(s.app).completeCodexPlanModelSet(action, modelID)
			case "model.plan_config.set_effort":
				return newModelConfigService(s.app).completeCodexPlanReasoningEffortSet(action, actionStringValue(action, "reasoning_effort"))
			case "model.plan_config.select_effort":
				reasoningEffort := strings.TrimSpace(action.Option)
				if reasoningEffort == modelConfigDefaultOptionValue {
					reasoningEffort = ""
				}
				return newModelConfigService(s.app).completeCodexPlanReasoningEffortSet(action, reasoningEffort)
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
					if groupBindingScopeActive(a, msg) {
						return newBindingService(a).commandFast(msg, args)
					}
					return commandFastProfileAware(a, msg, args)
				},
				Backends: map[string]func(fields []string) bool{
					backendClaude: nil,
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			if groupBindingSessionScopeActive(s.app, sessionKey) {
				svc := newBindingService(s.app)
				switch actionName {
				case "menu.fast":
					msg := commandMessageFromAction(s.app, action, sessionKey, "/fast config")
					binding, err := svc.ensureBindingForMessage(msg)
					if err != nil {
						return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
					}
					return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已打开当前群内响应速度配置"}, Card: rawCard(svc.renderBindingFastCard(sessionKey, binding))}, nil
				case "service_tier.set":
					return svc.completeBindingServiceTierSet(action, sessionKey, actionStringValue(action, "service_tier"))
				}
			}
			if p2pSessionScopeActive(s.app, sessionKey) && actionName == "service_tier.set" {
				return completeBotProfileServiceTierSet(s.app, action, actionStringValue(action, "service_tier"))
			}
			switch actionName {
			case "menu.fast":
				return newMenuActionService(s.app).completeMenuFast(action, sessionKey)
			case "service_tier.set":
				return newMenuActionService(s.app).completeServiceTierSet(action, sessionKey, actionStringValue(action, "thread_id"), actionStringValue(action, "service_tier"))
			default:
				return nil, nil
			}
		},
	}
}
