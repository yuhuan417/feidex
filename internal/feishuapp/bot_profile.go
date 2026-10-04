package feishuapp

import (
	"errors"
	"feidex/internal/adapter/feishu/backend"
	modelcommands "feidex/internal/adapter/feishu/modelconfig"
	domainbackend "feidex/internal/domain/backend"
	"fmt"
	"strings"

	applicationmodelconfig "feidex/internal/application/modelconfig"
	applicationrouting "feidex/internal/application/routing"
	"feidex/internal/domain/routing"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func commandWorkspaceProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	return commandWorkspace(a, msg, args)
}

func commandModelProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") || len(args) == 0 {
		return a.bindings.BackendConfiguration.HandleBackendModelCommand(msg, args)
	}
	if len(args) == 3 {
		setting := routing.Setting(strings.ToLower(strings.TrimSpace(args[0])))
		operation := strings.ToLower(strings.TrimSpace(args[1]))
		if operation == "set" && setting.Auxiliary() && setting != routing.PlanEffort && setting != routing.SubagentEffort {
			return saveAuxiliaryCommand(a, msg, setting, args[2], string(setting)+" model")
		}
		if operation == "effort" && a.configView().configuredBackend() == domainbackend.BackendCodex {
			switch setting {
			case routing.PlanModel:
				return saveAuxiliaryCommand(a, msg, routing.PlanEffort, args[2], "Plan reasoning effort")
			case routing.SubagentModel:
				return saveAuxiliaryCommand(a, msg, routing.SubagentEffort, args[2], "subagent reasoning effort")
			}
		}
	}
	return a.bindings.BackendConfiguration.HandleBackendModelCommand(msg, args)
}

func saveAuxiliaryCommand(a *App, msg *feishu.InboundMessage, setting routing.Setting, value, label string) error {
	result, err := a.bindings.ModelSettings.SaveAuxiliary(a.configView().makeSessionKey(msg), a.configView().configuredBackend(), setting, value)
	if err != nil {
		return err
	}
	scope := "Bot"
	if result.Scope == applicationmodelconfig.SessionScope {
		scope = "session"
	}
	return replyTextEffect(a, msg, "已更新当前 "+scope+" 的 "+label)
}

func commandEffortProfileAware(modelcommandsDep modelcommands.ModelConfigService, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") || len(args) == 0 {
		return modelcommandsDep.CommandEffort(msg, args)
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: /effort | /effort EFFORT|default")
	}
	return modelcommandsDep.CommandEffort(msg, args)
}

func commandFastProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") || (len(args) == 1 && strings.EqualFold(strings.TrimSpace(args[0]), "config")) {
		return commandFast(a.bindings.ServiceTier, msg, args)
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /fast | /fast fast | /fast default | /fast off | /fast toggle")
	}
	value := ""
	if len(args) == 1 {
		value = args[0]
	}
	toggle := len(args) == 0 || strings.EqualFold(strings.TrimSpace(value), "toggle")
	result, err := a.bindings.ScopedRoutingConfiguration.ChangeServiceTier(applicationrouting.Scope{ChatType: msg.ChatType, ChatID: msg.ChatID}, value, toggle)
	if err != nil {
		return err
	}
	updated := value
	if result.Profile != nil {
		updated = result.Profile.ServiceTier
	}
	return replyTextEffect(a, msg, "已更新当前 Bot 的默认响应速度: "+renderOptionalBacktick(updated))
}

func effectiveBotProfile(a *App) *state.BotProfile {
	if a == nil || a.State() == nil {
		return nil
	}
	return a.State().BotProfile()
}

func completeBotProfileModelSet(backendconfiguration backend.ConfigurationService, action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	return backendconfiguration.CompleteGlobalModelSet(action, modelID)
}

func completeBotProfileEffortSet(backendconfiguration backend.ConfigurationService, action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
	return backendconfiguration.CompleteGlobalReasoningEffortSet(action, effort)
}

func completeBotProfileAuxiliaryModelSet(a *App, action *feishu.CardAction, role, value string) (*callback.CardActionTriggerResponse, error) {
	result, err := a.bindings.ModelSettings.SaveAuxiliary(actionSessionKey(action), a.configView().configuredBackend(), routing.Setting(role), value)
	if err != nil {
		kind := "error"
		if errors.Is(err, applicationmodelconfig.ErrSaveBlocked) {
			kind = "warning"
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: kind, Content: err.Error()}}, nil
	}
	scope := "Bot"
	if result.Scope == applicationmodelconfig.SessionScope {
		scope = "session"
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已保存当前 " + scope + " 的辅助模型配置；待对应会话边界生效"}}, nil
}

func completeBotProfileServiceTierSet(a *App, action *feishu.CardAction, serviceTier string) (*callback.CardActionTriggerResponse, error) {
	_, err := a.bindings.ScopedRoutingConfiguration.ChangeServiceTier(applicationrouting.Scope{ChatType: "p2p", ChatID: "profile"}, serviceTier, false)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已更新当前 Bot 默认响应速度"}, Card: rawCard(renderServiceTierMenuCard(a.State(), actionSessionKey(action)))}, nil
}
