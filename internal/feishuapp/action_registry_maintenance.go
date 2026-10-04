package feishuapp

import (
	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func maintenanceCardActionHandlers() map[string]cardActionHandler {
	return map[string]cardActionHandler{
		"upgrade.dev": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newMenuActionService(s.app).completeUpgradeDev(action)
		},
	}
}

func backendUpgradeCommandCompleter(service cardActionService) upgradeActionCommandCompleter {
	return func(action *feishu.CardAction, sessionKey, rawCommand, toastText string, preparingCard map[string]any, failureCard func(string, string) map[string]any, patchLog string) (*callback.CardActionTriggerResponse, error) {
		return completeAsyncCommandAction(service.app, action, sessionKey, rawCommand, "menu.group.system", toastText, preparingCard, nil, failureCard, patchLog)
	}
}

func maintenancePortCardActionHandlers(upgrades appupgradecmd.UpgradeService, backendUpgrades backendUpgradeService, complete upgradeActionCommandCompleter) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
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
