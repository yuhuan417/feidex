package feishuapp

import (
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/adapter/feishu/quietmode"
	"feidex/internal/adapter/feishu/skills"
	"feidex/internal/application/runtimeconfig"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type ToolsCardActionInputs struct {
	CompleteMenuCommand func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
	Skills              *skills.Service
	Backend             func() string
	State               planmode.SessionStateProvider
	Renderer            bindingCardRenderer
	RuntimeSettings     runtimeconfig.Service
	QuietMode           func() config.QuietMode
}

func toolsCardActionHandlers(inputs ToolsCardActionInputs) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		"menu.quiet": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/quiet config", "menu.tools")
		},
		"quiet.set": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			mode := config.QuietMode(actionStringValue(action, "mode"))
			if err := updateQuietMode(inputs.RuntimeSettings, mode); err != nil {
				return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
			}
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "success", Content: "已更新 quiet 模式为 " + quietmode.StatusText(mode)},
				Card:  rawCard(renderQuietModeMenuCard(inputs.QuietMode(), sessionKey, planModeTitleForSession(inputs.State, true, sessionKey, "Quiet Mode"))),
			}, nil
		},
		"menu.history": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/history", "menu.tools")
		},
		"menu.usage": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/usage", "menu.tools")
		},
		"menu.download": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/download", "menu.tools")
		},
		"menu.skills": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if !menuActionVisibleForBackend("menu.skills", inputs.Backend()) {
				return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/skills", "menu.tools")
			}
			return inputs.Skills.CompleteSkillsOpen(action, actionSessionKey(action))
		},
		"skills.select": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.Skills.CompleteSkillsSelect(action, actionSessionKey(action), action.Option)
		},
		"skills.reload": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.Skills.CompleteSkillsReload(action, actionSessionKey(action))
		},
	}
}
