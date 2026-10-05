package feishuapp

import (
	"strings"

	appbackend "feidex/internal/adapter/feishu/backend"
	modelcommands "feidex/internal/adapter/feishu/modelconfig"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/modelconfig"
	applicationrouting "feidex/internal/application/routing"
	"feidex/internal/application/threadsettings"
	"feidex/internal/compositionkit"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type ModelCardActionInputs struct {
	Backend                    func() string
	BindingCommands            bindingService
	BackendConfiguration       appbackend.ConfigurationService
	ModelCommands              modelcommands.ModelConfigService
	ModelSettings              modelconfig.SettingsService
	ScopedRoutingConfiguration compositionkit.ScopedRoutingConfiguration
	ThreadSettings             threadsettings.Service
	State                      *appstate.Store
	CompleteMenuCommand        func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
}

func modelCardActionHandlers(inputs ModelCardActionInputs) map[string]cardActionPortHandler {
	handlers := map[string]cardActionPortHandler{
		"menu.model": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, sessionKey) {
				msg := commandMessageFromAction(inputs.BindingCommands.scope, action, sessionKey, "/model")
				binding, err := inputs.BindingCommands.deps.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
				if err != nil {
					return warningCard(err.Error()), nil
				}
				card, err := inputs.BindingCommands.renderBindingModelConfigCard(sessionKey, binding)
				if err != nil {
					return warningCard(err.Error()), nil
				}
				return infoCard("已打开当前群内模型配置", card), nil
			}
			return inputs.CompleteMenuCommand(action, sessionKey, "/model", "menu.group.model")
		},
		"menu.model_auxiliary": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, sessionKey) {
				msg := commandMessageFromAction(inputs.BindingCommands.scope, action, sessionKey, "/model")
				binding, err := inputs.BindingCommands.deps.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
				if err != nil {
					return warningCard(err.Error()), nil
				}
				card, err := inputs.BindingCommands.renderBindingAuxiliaryModelConfigCard(sessionKey, binding)
				if err != nil {
					return warningCard(err.Error()), nil
				}
				return infoCard("已打开当前群内辅助模型配置", card), nil
			}
			if inputs.Backend() == domainbackend.BackendClaude {
				return infoCard("已打开 Claude 辅助模型配置", inputs.ModelCommands.RenderClaudeAuxiliaryModelConfigCard(sessionKey, "menu.model_auxiliary")), nil
			}
			card, err := inputs.ModelCommands.RenderCodexAuxiliaryModelConfigCardForSession(sessionKey, "menu.model_auxiliary")
			if err != nil {
				return errorCard(err.Error()), nil
			}
			return infoCard("已打开 Codex 辅助模型配置", card), nil
		},
		"menu.fast": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, sessionKey) {
				msg := commandMessageFromAction(inputs.BindingCommands.scope, action, sessionKey, "/fast config")
				binding, err := inputs.BindingCommands.deps.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
				if err != nil {
					return warningCard(err.Error()), nil
				}
				return infoCard("已打开当前群内响应速度配置", inputs.BindingCommands.renderBindingFastCard(sessionKey, binding)), nil
			}
			return inputs.CompleteMenuCommand(action, sessionKey, "/fast config", "menu.group.model")
		},
		"service_tier.set": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			value := actionStringValue(action, "service_tier")
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, sessionKey) {
				return inputs.BindingCommands.completeBindingServiceTierSet(action, sessionKey, value)
			}
			if p2pSessionScopeActive(inputs.BindingCommands.scope, sessionKey) {
				_, err := inputs.ScopedRoutingConfiguration.ChangeServiceTier(applicationrouting.Scope{ChatType: "p2p", ChatID: "profile"}, value, false)
				if err != nil {
					return warningCard(err.Error()), nil
				}
				return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已更新当前 Bot 默认响应速度"}, Card: rawCard(renderServiceTierMenuCard(inputs.State, sessionKey))}, nil
			}
			threadID := actionStringValue(action, "thread_id")
			if _, err := setThreadServiceTier(inputs.ThreadSettings, sessionKey, threadID, value); err != nil {
				return warningCard(err.Error()), nil
			}
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已更新 service tier"}, Card: rawCard(renderServiceTierMenuCard(inputs.State, sessionKey))}, nil
		},
	}
	for _, name := range []string{"model.config.set_model", "model.config.select_model", "model.config.add_option", "model.config.remove_option", "model.config.set_effort", "model.config.select_effort", "model.plan_config.set_model", "model.plan_config.select_model", "model.plan_config.set_effort", "model.plan_config.select_effort", "model.aux_config.select_review_model", "model.aux_config.select_plan_model", "model.aux_config.select_plan_effort", "model.aux_config.select_subagent_model", "model.aux_config.select_subagent_effort", "model.aux_config.select_small_model"} {
		name := name
		handlers[name] = func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return modelCardAction(inputs, name, action)
		}
	}
	return handlers
}

func modelCardAction(inputs ModelCardActionInputs, name string, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	if groupBindingSessionScopeActive(inputs.BindingCommands.scope, sessionKey) {
		value := strings.TrimSpace(action.Option)
		if value == modelcommands.DefaultOptionValue {
			value = ""
		}
		role := map[string]string{"model.aux_config.select_review_model": "review", "model.aux_config.select_plan_model": "plan", "model.aux_config.select_plan_effort": "plan_effort", "model.aux_config.select_subagent_model": "subagent", "model.aux_config.select_subagent_effort": "subagent_effort", "model.aux_config.select_small_model": "small"}[name]
		svc := inputs.BindingCommands
		switch name {
		case "model.config.set_model":
			return svc.completeBindingModelSet(action, sessionKey, actionStringValue(action, "model_id"))
		case "model.config.select_model":
			return svc.completeBindingModelSet(action, sessionKey, value)
		case "model.config.set_effort":
			return svc.completeBindingEffortSet(action, sessionKey, actionStringValue(action, "reasoning_effort"))
		case "model.config.select_effort":
			return svc.completeBindingEffortSet(action, sessionKey, value)
		case "model.config.add_option":
			return svc.completeClaudeModelOption(action, sessionKey, true)
		case "model.config.remove_option":
			return svc.completeClaudeModelOption(action, sessionKey, false)
		case "model.plan_config.set_model", "model.plan_config.select_model", "model.plan_config.set_effort", "model.plan_config.select_effort":
			return warningCard("该项是 bot frontend 默认配置，请私聊该 bot 使用"), nil
		default:
			return svc.completeBindingAuxiliaryModelSet(action, sessionKey, role, value)
		}
	}
	if p2pSessionScopeActive(inputs.BindingCommands.scope, sessionKey) {
		value := strings.TrimSpace(action.Option)
		if value == modelcommands.DefaultOptionValue {
			value = ""
		}
		role := map[string]string{"model.aux_config.select_review_model": "review", "model.aux_config.select_plan_model": "plan", "model.aux_config.select_plan_effort": "plan_effort", "model.aux_config.select_subagent_model": "subagent", "model.aux_config.select_subagent_effort": "subagent_effort", "model.aux_config.select_small_model": "small", "model.plan_config.select_model": "plan", "model.plan_config.select_effort": "plan_effort"}[name]
		switch name {
		case "model.config.set_model":
			return completeBotProfileModelSet(inputs.BackendConfiguration, action, actionStringValue(action, "model_id"))
		case "model.config.select_model":
			return completeBotProfileModelSet(inputs.BackendConfiguration, action, value)
		case "model.config.set_effort":
			return completeBotProfileEffortSet(inputs.BackendConfiguration, action, actionStringValue(action, "reasoning_effort"))
		case "model.config.select_effort":
			return completeBotProfileEffortSet(inputs.BackendConfiguration, action, value)
		case "model.aux_config.select_plan_model", "model.aux_config.select_plan_effort", "model.aux_config.select_review_model", "model.aux_config.select_subagent_model", "model.aux_config.select_subagent_effort", "model.aux_config.select_small_model", "model.plan_config.select_model", "model.plan_config.select_effort":
			return completeBotProfileAuxiliaryModelSetWith(inputs.ModelSettings, inputs.Backend(), action, role, value)
		}
	}
	value := strings.TrimSpace(action.Option)
	if value == modelcommands.DefaultOptionValue {
		value = ""
	}
	switch name {
	case "model.config.set_model":
		return inputs.BackendConfiguration.CompleteGlobalModelSet(action, actionStringValue(action, "model_id"))
	case "model.config.select_model":
		return inputs.BackendConfiguration.CompleteGlobalModelSet(action, value)
	case "model.config.add_option":
		return inputs.BackendConfiguration.CompleteClaudeModelOptionAdd(action)
	case "model.config.remove_option":
		return inputs.BackendConfiguration.CompleteClaudeModelOptionRemove(action)
	case "model.config.set_effort":
		return inputs.BackendConfiguration.CompleteGlobalReasoningEffortSet(action, actionStringValue(action, "reasoning_effort"))
	case "model.config.select_effort":
		return inputs.BackendConfiguration.CompleteGlobalReasoningEffortSet(action, value)
	case "model.plan_config.set_model":
		return inputs.ModelCommands.CompleteCodexPlanModelSet(action, actionStringValue(action, "model_id"))
	case "model.plan_config.select_model":
		return inputs.ModelCommands.CompleteCodexPlanModelSet(action, value)
	case "model.plan_config.set_effort":
		return inputs.ModelCommands.CompleteCodexPlanReasoningEffortSet(action, actionStringValue(action, "reasoning_effort"))
	case "model.plan_config.select_effort":
		return inputs.ModelCommands.CompleteCodexPlanReasoningEffortSet(action, value)
	case "model.aux_config.select_review_model":
		return inputs.ModelCommands.CompleteCodexAuxiliaryModelSet(action, "review", value)
	case "model.aux_config.select_subagent_model":
		if inputs.Backend() == domainbackend.BackendClaude {
			return inputs.ModelCommands.CompleteClaudeAuxiliaryModelSet(action, "subagent", value)
		}
		return inputs.ModelCommands.CompleteCodexAuxiliaryModelSet(action, "subagent", value)
	case "model.aux_config.select_subagent_effort":
		return inputs.ModelCommands.CompleteCodexAuxiliaryModelSet(action, "subagent_effort", value)
	case "model.aux_config.select_small_model":
		return inputs.ModelCommands.CompleteClaudeAuxiliaryModelSet(action, "small", value)
	default:
		return nil, nil
	}
}

func warningCard(text string) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: text}}
}
func errorCard(text string) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: text}}
}
func infoCard(text string, card map[string]any) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: text}, Card: rawCard(card)}
}
