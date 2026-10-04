package feishuapp

import (
	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func maintenanceCardActionHandlers(upgrades appupgradecmd.UpgradeService, backendUpgrades backendUpgradeService) map[string]cardActionHandler {
	return map[string]cardActionHandler{
		"upgrade.confirm": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return upgrades.CompleteUpgradeAction(action, "upgrade.confirm")
		},
		"upgrade.cancel": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return upgrades.CompleteUpgradeAction(action, "upgrade.cancel")
		},
		"upgrade.local.pick": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return upgrades.CompleteUpgradeLocalPick(action)
		},
		"upgrade.dev": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newMenuActionService(s.app).completeUpgradeDev(action)
		},
		"codex_upgrade.refresh": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeRefresh(backendUpgradeCodex, action)
		},
		"codex_upgrade.check": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeCheck(backendUpgradeCodex, action)
		},
		"codex_upgrade.prepare": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradePrepare(backendUpgradeCodex, action)
		},
		"codex_restart.run": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeRestartRun(backendUpgradeCodex, action)
		},
		"codex_upgrade.confirm": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeAction(backendUpgradeCodex, action, "codex_upgrade.confirm")
		},
		"codex_upgrade.cancel": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeAction(backendUpgradeCodex, action, "codex_upgrade.cancel")
		},
		"claude_upgrade.refresh": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeRefresh(backendUpgradeClaude, action)
		},
		"claude_upgrade.check": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeCheck(backendUpgradeClaude, action)
		},
		"claude_upgrade.prepare": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradePrepare(backendUpgradeClaude, action)
		},
		"claude_restart.run": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeRestartRun(backendUpgradeClaude, action)
		},
		"claude_upgrade.confirm": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeAction(backendUpgradeClaude, action, "claude_upgrade.confirm")
		},
		"claude_upgrade.cancel": func(_ cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return backendUpgrades.completeUpgradeAction(backendUpgradeClaude, action, "claude_upgrade.cancel")
		},
	}

}
