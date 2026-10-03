package feishuapp

import (
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func maintenanceCardActionHandlers() map[string]cardActionHandler {
	return map[string]cardActionHandler{
		"upgrade.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.Upgrades.CompleteUpgradeAction(action, "upgrade.confirm")
		},
		"upgrade.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.Upgrades.CompleteUpgradeAction(action, "upgrade.cancel")
		},
		"upgrade.local.pick": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.Upgrades.CompleteUpgradeLocalPick(action)
		},
		"upgrade.dev": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newMenuActionService(s.app).completeUpgradeDev(action)
		},
		"codex_upgrade.refresh": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeUpgradeRefresh(backendUpgradeCodex, action)
		},
		"codex_upgrade.check": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeUpgradeCheck(backendUpgradeCodex, action)
		},
		"codex_upgrade.prepare": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeUpgradePrepare(backendUpgradeCodex, action)
		},
		"codex_restart.run": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeRestartRun(backendUpgradeCodex, action)
		},
		"codex_upgrade.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeUpgradeAction(backendUpgradeCodex, action, "codex_upgrade.confirm")
		},
		"codex_upgrade.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeUpgradeAction(backendUpgradeCodex, action, "codex_upgrade.cancel")
		},
		"claude_upgrade.refresh": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeUpgradeRefresh(backendUpgradeClaude, action)
		},
		"claude_upgrade.check": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeUpgradeCheck(backendUpgradeClaude, action)
		},
		"claude_upgrade.prepare": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeUpgradePrepare(backendUpgradeClaude, action)
		},
		"claude_restart.run": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeRestartRun(backendUpgradeClaude, action)
		},
		"claude_upgrade.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeUpgradeAction(backendUpgradeClaude, action, "claude_upgrade.confirm")
		},
		"claude_upgrade.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BackendUpgrades.completeUpgradeAction(backendUpgradeClaude, action, "claude_upgrade.cancel")
		},
	}

}
