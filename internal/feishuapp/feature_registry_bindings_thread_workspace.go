package feishuapp

import (
	domainbackend "feidex/internal/domain/backend"
	"fmt"
	"strings"

	"feidex/internal/adapter/feishu/modelconfig"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func appendFeatureBindingsThreadWorkspace(bindings map[string]featureBinding) {
	bindings["menu.interrupt"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"interrupt": {
				Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
					return a.bindings.ThreadMenu.CommandInterrupt(msg)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if actionName != "menu.interrupt" {
				return nil, nil
			}
			return s.app.bindings.ThreadMenu.CompleteMenuInterrupt(action, actionSessionKey(action), actionStringValue(action, "turn_id"))
		},
	}
	bindings["menu.thread"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"fork": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return commandFork(a, msg, args)
				},
			},
			"new": {
				Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
					return a.bindings.ThreadMenu.CommandThreadsNew(msg)
				},
			},
			"thread": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.ThreadMenu.CommandThread(msg, args)
				},
			},
			"session": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.ThreadMenu.CommandSession(msg, args)
				},
			},
			"threads": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if len(args) > 0 {
						return fmt.Errorf("usage: /threads")
					}
					return a.bindings.ThreadMenu.CommandThread(msg, []string{"list"})
				},
			},
		},
		RenderActions: []string{"menu.thread"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.thread" {
				return nil, false
			}
			sessionKey = threadMenuEffectiveSessionKey(a.configView().normalizeSessionKey, a.bindings.BindingCommands.scope, a.bindings.ConversationQuery, sessionKey)
			backend := ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex())
			card, err := renderThreadsCard(threadCardInputs{
				Repository: a.State(), Config: a.Config(), Backend: backend, Conversations: a.bindings.Conversations,
			}, sessionKey, false)
			if err != nil {
				return nil, false
			}
			return card, true
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			switch actionName {
			case "menu.thread":
				return s.app.bindings.ThreadMenu.CompleteMenuThread(action, sessionKey)
			case "menu.new":
				return s.app.bindings.ThreadMenu.CompleteMenuNew(action, sessionKey)
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
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if groupBindingScopeActive(msg) {
						return a.bindings.BindingCommands.commandWorkspace(msg, args)
					}
					return commandWorkspace(a, msg, args)
				},
			},
		},
		RenderActions: []string{"menu.workspace"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.workspace" {
				return nil, false
			}
			return a.bindings.WorkspacePresentation.RenderWorkspaceMenuCard(sessionKey), true
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
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if groupBindingScopeActive(msg) {
						return a.bindings.BindingCommands.commandModel(msg, args)
					}
					return commandModelProfileAware(a, msg, args)
				},
			},
			"effort": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if groupBindingScopeActive(msg) {
						return a.bindings.BindingCommands.commandEffort(msg, args)
					}
					return commandEffortProfileAware(a.bindings.ModelCommands, msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, sessionKey) {
				svc := s.app.bindings.BindingCommands
				switch actionName {
				case "menu.model_auxiliary":
					msg := commandMessageFromAction(s.app.bindings.BindingCommands.scope, action, sessionKey, "/model")
					binding, err := svc.app.bindings.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
					if err != nil {
						return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
					}
					card, err := svc.renderBindingAuxiliaryModelConfigCard(sessionKey, binding)
					if err != nil {
						return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
					}
					return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已打开当前群内辅助模型配置"}, Card: rawCard(card)}, nil
				case "model.aux_config.select_plan_model", "model.aux_config.select_plan_effort", "model.aux_config.select_review_model", "model.aux_config.select_subagent_model", "model.aux_config.select_subagent_effort", "model.aux_config.select_small_model":
					role := map[string]string{
						"model.aux_config.select_plan_model":      "plan",
						"model.aux_config.select_plan_effort":     "plan_effort",
						"model.aux_config.select_review_model":    "review",
						"model.aux_config.select_subagent_model":  "subagent",
						"model.aux_config.select_subagent_effort": "subagent_effort",
						"model.aux_config.select_small_model":     "small",
					}[actionName]
					value := strings.TrimSpace(action.Option)
					if value == modelconfig.DefaultOptionValue {
						value = ""
					}
					return svc.completeBindingAuxiliaryModelSet(action, sessionKey, role, value)
				case "menu.model":
					msg := commandMessageFromAction(s.app.bindings.BindingCommands.scope, action, sessionKey, "/model")
					binding, err := svc.app.bindings.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
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
					if modelID == modelconfig.DefaultOptionValue {
						modelID = ""
					}
					return svc.completeBindingModelSet(action, sessionKey, modelID)
				case "model.config.set_effort":
					return svc.completeBindingEffortSet(action, sessionKey, actionStringValue(action, "reasoning_effort"))
				case "model.config.select_effort":
					reasoningEffort := strings.TrimSpace(action.Option)
					if reasoningEffort == modelconfig.DefaultOptionValue {
						reasoningEffort = ""
					}
					return svc.completeBindingEffortSet(action, sessionKey, reasoningEffort)
				case "model.config.add_option":
					return svc.completeClaudeModelOption(action, sessionKey, true)
				case "model.config.remove_option":
					return svc.completeClaudeModelOption(action, sessionKey, false)
				case "model.plan_config.set_model", "model.plan_config.select_model", "model.plan_config.set_effort", "model.plan_config.select_effort":
					return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "该项是 bot frontend 默认配置，请私聊该 bot 使用"}}, nil
				}
			}
			if p2pSessionScopeActive(s.app.bindings.BindingCommands.scope, sessionKey) {
				switch actionName {
				case "model.aux_config.select_plan_model", "model.aux_config.select_plan_effort", "model.aux_config.select_review_model", "model.aux_config.select_subagent_model", "model.aux_config.select_subagent_effort", "model.aux_config.select_small_model", "model.plan_config.select_model", "model.plan_config.select_effort":
					role := map[string]string{
						"model.aux_config.select_plan_model":      "plan",
						"model.aux_config.select_plan_effort":     "plan_effort",
						"model.aux_config.select_review_model":    "review",
						"model.aux_config.select_subagent_model":  "subagent",
						"model.aux_config.select_subagent_effort": "subagent_effort",
						"model.aux_config.select_small_model":     "small",
						"model.plan_config.select_model":          "plan",
						"model.plan_config.select_effort":         "plan_effort",
					}[actionName]
					value := strings.TrimSpace(action.Option)
					if value == modelconfig.DefaultOptionValue {
						value = ""
					}
					return completeBotProfileAuxiliaryModelSet(s.app, action, role, value)
				case "model.config.set_model":
					return completeBotProfileModelSet(s.app.bindings.BackendConfiguration, action, actionStringValue(action, "model_id"))
				case "model.config.select_model":
					modelID := strings.TrimSpace(action.Option)
					if modelID == modelconfig.DefaultOptionValue {
						modelID = ""
					}
					return completeBotProfileModelSet(s.app.bindings.BackendConfiguration, action, modelID)
				case "model.config.set_effort":
					return completeBotProfileEffortSet(s.app.bindings.BackendConfiguration, action, actionStringValue(action, "reasoning_effort"))
				case "model.config.select_effort":
					effort := strings.TrimSpace(action.Option)
					if effort == modelconfig.DefaultOptionValue {
						effort = ""
					}
					return completeBotProfileEffortSet(s.app.bindings.BackendConfiguration, action, effort)
				}
			}
			if actionName == "menu.model_auxiliary" {
				if s.app.configView().configuredBackend() == domainbackend.BackendClaude {
					return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已打开 Claude 辅助模型配置"}, Card: rawCard(s.app.bindings.ModelCommands.RenderClaudeAuxiliaryModelConfigCard(sessionKey, "menu.model_auxiliary"))}, nil
				}
				card, err := s.app.bindings.ModelCommands.RenderCodexAuxiliaryModelConfigCardForSession(sessionKey, "menu.model_auxiliary")
				if err != nil {
					return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
				}
				return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已打开 Codex 辅助模型配置"}, Card: rawCard(card)}, nil
			}
			switch actionName {
			case "model.aux_config.select_review_model":
				value := strings.TrimSpace(action.Option)
				if value == modelconfig.DefaultOptionValue {
					value = ""
				}
				return s.app.bindings.ModelCommands.CompleteCodexAuxiliaryModelSet(action, "review", value)
			case "model.aux_config.select_subagent_model":
				value := strings.TrimSpace(action.Option)
				if value == modelconfig.DefaultOptionValue {
					value = ""
				}
				if s.app.configView().configuredBackend() == domainbackend.BackendClaude {
					return s.app.bindings.ModelCommands.CompleteClaudeAuxiliaryModelSet(action, "subagent", value)
				}
				return s.app.bindings.ModelCommands.CompleteCodexAuxiliaryModelSet(action, "subagent", value)
			case "model.aux_config.select_subagent_effort":
				value := strings.TrimSpace(action.Option)
				if value == modelconfig.DefaultOptionValue {
					value = ""
				}
				return s.app.bindings.ModelCommands.CompleteCodexAuxiliaryModelSet(action, "subagent_effort", value)
			case "model.aux_config.select_small_model":
				value := strings.TrimSpace(action.Option)
				if value == modelconfig.DefaultOptionValue {
					value = ""
				}
				return s.app.bindings.ModelCommands.CompleteClaudeAuxiliaryModelSet(action, "small", value)
			case "menu.model":
				return s.completeMenuModel(action, sessionKey)
			case "model.config.set_model":
				return s.app.bindings.BackendConfiguration.CompleteGlobalModelSet(action, actionStringValue(action, "model_id"))
			case "model.config.select_model":
				modelID := strings.TrimSpace(action.Option)
				if modelID == modelconfig.DefaultOptionValue {
					modelID = ""
				}
				return s.app.bindings.BackendConfiguration.CompleteGlobalModelSet(action, modelID)
			case "model.config.add_option":
				return s.app.bindings.BackendConfiguration.CompleteClaudeModelOptionAdd(action)
			case "model.config.remove_option":
				return s.app.bindings.BackendConfiguration.CompleteClaudeModelOptionRemove(action)
			case "model.config.set_effort":
				return s.app.bindings.BackendConfiguration.CompleteGlobalReasoningEffortSet(action, actionStringValue(action, "reasoning_effort"))
			case "model.config.select_effort":
				reasoningEffort := strings.TrimSpace(action.Option)
				if reasoningEffort == modelconfig.DefaultOptionValue {
					reasoningEffort = ""
				}
				return s.app.bindings.BackendConfiguration.CompleteGlobalReasoningEffortSet(action, reasoningEffort)
			case "model.plan_config.set_model":
				return s.app.bindings.ModelCommands.CompleteCodexPlanModelSet(action, actionStringValue(action, "model_id"))
			case "model.plan_config.select_model":
				modelID := strings.TrimSpace(action.Option)
				if modelID == modelconfig.DefaultOptionValue {
					modelID = ""
				}
				return s.app.bindings.ModelCommands.CompleteCodexPlanModelSet(action, modelID)
			case "model.plan_config.set_effort":
				return s.app.bindings.ModelCommands.CompleteCodexPlanReasoningEffortSet(action, actionStringValue(action, "reasoning_effort"))
			case "model.plan_config.select_effort":
				reasoningEffort := strings.TrimSpace(action.Option)
				if reasoningEffort == modelconfig.DefaultOptionValue {
					reasoningEffort = ""
				}
				return s.app.bindings.ModelCommands.CompleteCodexPlanReasoningEffortSet(action, reasoningEffort)
			default:
				return nil, nil
			}
		},
	}
	bindings["menu.fast"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"fast": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if groupBindingScopeActive(msg) {
						return a.bindings.BindingCommands.commandFast(msg, args)
					}
					return commandFastProfileAware(a, msg, args)
				},
			},
		},
		HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, sessionKey) {
				svc := s.app.bindings.BindingCommands
				switch actionName {
				case "menu.fast":
					msg := commandMessageFromAction(s.app.bindings.BindingCommands.scope, action, sessionKey, "/fast config")
					binding, err := svc.app.bindings.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
					if err != nil {
						return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
					}
					return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已打开当前群内响应速度配置"}, Card: rawCard(svc.renderBindingFastCard(sessionKey, binding))}, nil
				case "service_tier.set":
					return svc.completeBindingServiceTierSet(action, sessionKey, actionStringValue(action, "service_tier"))
				}
			}
			if p2pSessionScopeActive(s.app.bindings.BindingCommands.scope, sessionKey) && actionName == "service_tier.set" {
				return completeBotProfileServiceTierSet(s.app, action, actionStringValue(action, "service_tier"))
			}
			switch actionName {
			case "menu.fast":
				return s.completeMenuFast(action, sessionKey)
			case "service_tier.set":
				return s.completeServiceTierSet(action, sessionKey, actionStringValue(action, "thread_id"), actionStringValue(action, "service_tier"))
			default:
				return nil, nil
			}
		},
	}
}
