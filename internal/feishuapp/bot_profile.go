package feishuapp

import (
	"errors"
	"feidex/internal/adapter/feishu/backend"
	modelcommands "feidex/internal/adapter/feishu/modelconfig"
	tier "feidex/internal/adapter/feishu/servicetier"
	domainbackend "feidex/internal/domain/backend"
	"fmt"
	"strings"

	appstate "feidex/internal/adapter/storage/json/scoped"
	applicationmodelconfig "feidex/internal/application/modelconfig"
	applicationrouting "feidex/internal/application/routing"
	"feidex/internal/compositionkit"
	"feidex/internal/domain/routing"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type ProfileCommandInputs struct {
	BackendConfiguration       backend.ConfigurationService
	ModelSettings              applicationmodelconfig.SettingsService
	ScopedRoutingConfiguration compositionkit.ScopedRoutingConfiguration
	ServiceTier                tier.Service
	ConfiguredBackend          func() string
	MakeSessionKey             func(*feishu.InboundMessage) string
	Effects                    frontendruntime.EffectRunner
	FrontendID                 string
	ReplyInThread              bool
}

func handleModelProfileCommand(inputs ProfileCommandInputs, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") || len(args) == 0 {
		return inputs.BackendConfiguration.HandleBackendModelCommand(msg, args)
	}
	if len(args) == 3 {
		setting := routing.Setting(strings.ToLower(strings.TrimSpace(args[0])))
		operation := strings.ToLower(strings.TrimSpace(args[1]))
		if operation == "set" && setting.Auxiliary() && setting != routing.PlanEffort && setting != routing.SubagentEffort {
			return saveAuxiliaryProfileCommand(inputs, msg, setting, args[2], string(setting)+" model")
		}
		if operation == "effort" && inputs.ConfiguredBackend() == domainbackend.BackendCodex {
			switch setting {
			case routing.PlanModel:
				return saveAuxiliaryProfileCommand(inputs, msg, routing.PlanEffort, args[2], "Plan reasoning effort")
			case routing.SubagentModel:
				return saveAuxiliaryProfileCommand(inputs, msg, routing.SubagentEffort, args[2], "subagent reasoning effort")
			}
		}
	}
	return inputs.BackendConfiguration.HandleBackendModelCommand(msg, args)
}

func saveAuxiliaryProfileCommand(inputs ProfileCommandInputs, msg *feishu.InboundMessage, setting routing.Setting, value, label string) error {
	result, err := inputs.ModelSettings.SaveAuxiliary(inputs.MakeSessionKey(msg), inputs.ConfiguredBackend(), setting, value)
	if err != nil {
		return err
	}
	scope := "Bot"
	if result.Scope == applicationmodelconfig.SessionScope {
		scope = "session"
	}
	return replyTextEffect(inputs.Effects, inputs.FrontendID, inputs.ReplyInThread, msg, "已更新当前 "+scope+" 的 "+label)
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

func handleFastProfileCommand(inputs ProfileCommandInputs, msg *feishu.InboundMessage, args []string) error {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") || (len(args) == 1 && strings.EqualFold(strings.TrimSpace(args[0]), "config")) {
		return commandFast(inputs.ServiceTier, msg, args)
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /fast | /fast fast | /fast default | /fast off | /fast toggle")
	}
	value := ""
	if len(args) == 1 {
		value = args[0]
	}
	toggle := len(args) == 0 || strings.EqualFold(strings.TrimSpace(value), "toggle")
	result, err := inputs.ScopedRoutingConfiguration.ChangeServiceTier(applicationrouting.Scope{ChatType: msg.ChatType, ChatID: msg.ChatID}, value, toggle)
	if err != nil {
		return err
	}
	updated := value
	if result.Profile != nil {
		updated = result.Profile.ServiceTier
	}
	return replyTextEffect(inputs.Effects, inputs.FrontendID, inputs.ReplyInThread, msg, "已更新当前 Bot 的默认响应速度: "+renderOptionalBacktick(updated))
}

func effectiveBotProfile(store *appstate.Store) *state.BotProfile {
	if store == nil {
		return nil
	}
	return store.BotProfile()
}

func completeBotProfileModelSet(backendconfiguration backend.ConfigurationService, action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	return backendconfiguration.CompleteGlobalModelSet(action, modelID)
}

func completeBotProfileEffortSet(backendconfiguration backend.ConfigurationService, action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
	return backendconfiguration.CompleteGlobalReasoningEffortSet(action, effort)
}

func completeBotProfileAuxiliaryModelSetWith(settings applicationmodelconfig.SettingsService, backend string, action *feishu.CardAction, role, value string) (*callback.CardActionTriggerResponse, error) {
	result, err := settings.SaveAuxiliary(actionSessionKey(action), backend, routing.Setting(role), value)
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
