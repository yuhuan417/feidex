package app

import (
	"errors"
	"fmt"
	"strings"

	appservicetiercmd "feidex/internal/adapter/feishu/servicetier"
	applicationmodelconfig "feidex/internal/application/modelconfig"
	"feidex/internal/domain/routing"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func commandWorkspaceProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	if err := commandWorkspace(a, msg, args); err != nil {
		return err
	}
	if msg == nil || len(args) == 0 || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") {
		return nil
	}
	if len(args) >= 2 {
		setting := routing.Setting(strings.ToLower(strings.TrimSpace(args[0])))
		if setting == "permission" {
			setting = routing.Permissions
		}
		switch setting {
		case routing.Sandbox, routing.ApprovalPolicy, routing.MultiAgent, routing.Permissions:
			_, err := newRoutingConfiguration(a).SetProfile(configuredBackend(a), setting, args[1])
			return err
		}
	}
	_, sess, ws := currentWorkspaceForMessage(a, msg)
	workspaceID := ""
	if ws != nil {
		workspaceID = ws.ID
	} else if sess != nil {
		workspaceID = strings.TrimSpace(sess.WorkspaceID)
	}
	if workspaceID == "" {
		return nil
	}
	_, err := newRoutingConfiguration(a).SetProfile(configuredBackend(a), routing.Workspace, workspaceID)
	return err
}

func commandModelProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") || len(args) == 0 {
		return newBackendConfigurationService(a).handleBackendModelCommand(msg, args)
	}
	if len(args) == 3 {
		setting := routing.Setting(strings.ToLower(strings.TrimSpace(args[0])))
		operation := strings.ToLower(strings.TrimSpace(args[1]))
		if operation == "set" && setting.Auxiliary() && setting != routing.PlanEffort && setting != routing.SubagentEffort {
			return saveAuxiliaryCommand(a, msg, setting, args[2], string(setting)+" model")
		}
		if operation == "effort" && configuredBackend(a) == backendCodex {
			switch setting {
			case routing.PlanModel:
				return saveAuxiliaryCommand(a, msg, routing.PlanEffort, args[2], "Plan reasoning effort")
			case routing.SubagentModel:
				return saveAuxiliaryCommand(a, msg, routing.SubagentEffort, args[2], "subagent reasoning effort")
			}
		}
	}
	if err := newBackendConfigurationService(a).handleBackendModelCommand(msg, args); err != nil {
		return err
	}
	if len(args) == 2 && strings.EqualFold(strings.TrimSpace(args[0]), "set") {
		_, err := newRoutingConfiguration(a).SetProfile(configuredBackend(a), routing.Model, args[1])
		return err
	}
	return nil
}

func saveAuxiliaryCommand(a *App, msg *feishu.InboundMessage, setting routing.Setting, value, label string) error {
	result, err := newModelSettingsService(a).SaveAuxiliary(makeSessionKey(a, msg), configuredBackend(a), setting, value)
	if err != nil {
		return err
	}
	scope := "Bot"
	if result.Scope == applicationmodelconfig.SessionScope {
		scope = "session"
	}
	return replyTextEffect(a, msg, "已更新当前 "+scope+" 的 "+label)
}

func commandEffortProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") || len(args) == 0 {
		return newModelConfigService(a).commandEffort(msg, args)
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: /effort | /effort EFFORT|default")
	}
	if err := newModelConfigService(a).commandEffort(msg, args); err != nil {
		return err
	}
	_, err := newRoutingConfiguration(a).SetProfile(configuredBackend(a), routing.Effort, args[0])
	return err
}

func commandFastProfileAware(a *App, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") || (len(args) == 1 && strings.EqualFold(strings.TrimSpace(args[0]), "config")) {
		return commandFast(a, msg, args)
	}
	value := ""
	if len(args) == 0 || (len(args) == 1 && strings.EqualFold(strings.TrimSpace(args[0]), "toggle")) {
		profile, err := newRoutingConfiguration(a).EnsureProfile()
		if err != nil {
			return err
		}
		value = appservicetiercmd.ToggleServiceTier(profile.ServiceTier)
	} else if len(args) == 1 {
		value = strings.ToLower(strings.TrimSpace(args[0]))
		if value == "off" || value == "default" {
			value = ""
		} else {
			value = appservicetiercmd.NormalizeServiceTier(value)
			if value == "" {
				return fmt.Errorf("unsupported service tier %q", args[0])
			}
		}
	} else {
		return fmt.Errorf("usage: /fast | /fast fast | /fast default | /fast off | /fast toggle")
	}
	_, err := newRoutingConfiguration(a).SetProfile(configuredBackend(a), routing.ServiceTier, value)
	if err != nil {
		return err
	}
	return replyTextEffect(a, msg, "已更新当前 Bot 的默认响应速度: "+renderOptionalBacktick(value))
}

func effectiveBotProfile(a *App) *state.BotProfile {
	if a == nil || a.State() == nil {
		return nil
	}
	return a.State().BotProfile()
}

func completeBotProfileModelSet(a *App, action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	resp, err := newBackendConfigurationService(a).completeGlobalModelSet(action, modelID)
	if err != nil || resp == nil || (resp.Toast != nil && strings.EqualFold(resp.Toast.Type, "error")) {
		return resp, err
	}
	_, err = newRoutingConfiguration(a).SetProfile(configuredBackend(a), routing.Model, modelID)
	return resp, err
}

func completeBotProfileEffortSet(a *App, action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
	resp, err := newBackendConfigurationService(a).completeGlobalReasoningEffortSet(action, effort)
	if err != nil || resp == nil || (resp.Toast != nil && strings.EqualFold(resp.Toast.Type, "error")) {
		return resp, err
	}
	_, err = newRoutingConfiguration(a).SetProfile(configuredBackend(a), routing.Effort, effort)
	return resp, err
}

func completeBotProfileAuxiliaryModelSet(a *App, action *feishu.CardAction, role, value string) (*callback.CardActionTriggerResponse, error) {
	result, err := newModelSettingsService(a).SaveAuxiliary(actionSessionKey(action), configuredBackend(a), routing.Setting(role), value)
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
	value := appservicetiercmd.NormalizeServiceTier(serviceTier)
	if strings.EqualFold(strings.TrimSpace(serviceTier), "default") || strings.EqualFold(strings.TrimSpace(serviceTier), "off") {
		value = ""
	}
	if strings.TrimSpace(serviceTier) != "" && value == "" && !strings.EqualFold(strings.TrimSpace(serviceTier), "default") && !strings.EqualFold(strings.TrimSpace(serviceTier), "off") {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "unsupported service tier"}}, nil
	}
	_, err := newRoutingConfiguration(a).SetProfile(configuredBackend(a), routing.ServiceTier, value)
	if err != nil {
		return nil, err
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已更新当前 Bot 默认响应速度"}, Card: rawCard(renderServiceTierMenuCard(a, actionSessionKey(action)))}, nil
}
