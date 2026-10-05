package feishuapp

import (
	appbackend "feidex/internal/adapter/feishu/backend"
	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type MaintenanceCardActionInputs struct {
	Upgrades        appupgradecmd.UpgradeService
	BackendUpgrades backendUpgradeService
	BackendActions  appbackend.ActionService
}

func backendUpgradeCommandCompleter(service appbackend.ActionService) upgradeActionCommandCompleter {
	return func(action *feishu.CardAction, sessionKey, rawCommand, toastText string, preparingCard map[string]any, failureCard func(string, string) map[string]any, patchLog string) (*callback.CardActionTriggerResponse, error) {
		return service.CompleteAsyncCommandAction(action, sessionKey, rawCommand, "menu.group.system", toastText, preparingCard, nil, failureCard, patchLog)
	}
}

func maintenanceCardActionHandlers(inputs MaintenanceCardActionInputs) map[string]cardActionPortHandler {
	upgrades := inputs.Upgrades
	backendUpgrades := inputs.BackendUpgrades
	complete := backendUpgradeCommandCompleter(inputs.BackendActions)
	return map[string]cardActionPortHandler{
		"menu.upgrade": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			return inputs.BackendActions.CompleteAsyncCommandAction(
				action, sessionKey, "/upgrade", "menu.group.system", "正在检查可升级版本",
				upgrades.RenderUpgradePreparingCard(sessionKey), nil, upgrades.RenderUpgradeFailedCard, "upgrade panel patch failed",
			)
		},
		"upgrade.dev": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			sessionKey := actionSessionKey(action)
			return inputs.BackendActions.CompleteAsyncCommandAction(
				action,
				sessionKey,
				"/upgrade dev",
				"menu.group.system",
				"正在检查开发版升级信息",
				upgrades.RenderUpgradePreparingCard(sessionKey),
				nil,
				upgrades.RenderUpgradeFailedCard,
				"upgrade dev patch failed",
			)
		},
		"upgrade.confirm": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return upgrades.CompleteUpgradeAction(action, "upgrade.confirm")
		},
		"upgrade.cancel": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return upgrades.CompleteUpgradeAction(action, "upgrade.cancel")
		},
		"upgrade.local.pick": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return upgrades.CompleteUpgradeLocalPick(action)
		},
		"codex_upgrade.refresh": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeRefresh(backendUpgradeCodex, action, complete)
		},
		"codex_upgrade.check": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeCheck(backendUpgradeCodex, action, complete)
		},
		"codex_upgrade.prepare": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradePrepare(backendUpgradeCodex, action, complete)
		},
		"codex_restart.run": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeRestartRun(backendUpgradeCodex, action)
		},
		"codex_upgrade.confirm": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeAction(backendUpgradeCodex, action, "codex_upgrade.confirm")
		},
		"codex_upgrade.cancel": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeAction(backendUpgradeCodex, action, "codex_upgrade.cancel")
		},
		"claude_upgrade.refresh": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeRefresh(backendUpgradeClaude, action, complete)
		},
		"claude_upgrade.check": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeCheck(backendUpgradeClaude, action, complete)
		},
		"claude_upgrade.prepare": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradePrepare(backendUpgradeClaude, action, complete)
		},
		"claude_restart.run": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeRestartRun(backendUpgradeClaude, action)
		},
		"claude_upgrade.confirm": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeAction(backendUpgradeClaude, action, "claude_upgrade.confirm")
		},
		"claude_upgrade.cancel": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeAction(backendUpgradeClaude, action, "claude_upgrade.cancel")
		},
	}
}
