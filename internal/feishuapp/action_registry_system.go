package feishuapp

import (
	appbackend "feidex/internal/adapter/feishu/backend"
	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type SystemCardActionInputs struct {
	Debug               appdebugviewcmd.DebugService
	BackendUpgrades     backendUpgradeService
	BackendActions      appbackend.ActionService
	CompleteMenuCommand func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
}

func systemCardActionHandlers(inputs SystemCardActionInputs) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		"menu.debug": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.Debug.CompleteMenuDebug(action, actionSessionKey(action))
		},
		"menu.debug.logs": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.Debug.CompleteMenuDebugLogs(action, actionSessionKey(action))
		},
		"menu.status": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/status", "menu.group.system")
		},
		"menu.help": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/help", "menu.group.system")
		},
		"menu.codex_upgrade": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.BackendUpgrades.completeMenuUpgrade(backendUpgradeCodex, action, backendUpgradeCommandCompleter(inputs.BackendActions))
		},
		"menu.claude_upgrade": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.BackendUpgrades.completeMenuUpgrade(backendUpgradeClaude, action, backendUpgradeCommandCompleter(inputs.BackendActions))
		},
	}
}
