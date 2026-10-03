package feishuapp

import (
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func maintenanceCardActionHandlers() map[string]cardActionHandler {
	return map[string]cardActionHandler{
		"upgrade.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newUpgradeService(s.app).CompleteUpgradeAction(action, "upgrade.confirm")
		},
		"upgrade.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newUpgradeService(s.app).CompleteUpgradeAction(action, "upgrade.cancel")
		},
		"upgrade.local.pick": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newUpgradeService(s.app).CompleteUpgradeLocalPick(action)
		},
		"upgrade.dev": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newMenuActionService(s.app).completeUpgradeDev(action)
		},
		"codex_upgrade.refresh": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeUpgradeRefresh(backendUpgradeCodex, action)
		},
		"codex_upgrade.check": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeUpgradeCheck(backendUpgradeCodex, action)
		},
		"codex_upgrade.prepare": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeUpgradePrepare(backendUpgradeCodex, action)
		},
		"codex_restart.run": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeRestartRun(backendUpgradeCodex, action)
		},
		"codex_upgrade.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeUpgradeAction(backendUpgradeCodex, action, "codex_upgrade.confirm")
		},
		"codex_upgrade.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeUpgradeAction(backendUpgradeCodex, action, "codex_upgrade.cancel")
		},
		"claude_upgrade.refresh": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeUpgradeRefresh(backendUpgradeClaude, action)
		},
		"claude_upgrade.check": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeUpgradeCheck(backendUpgradeClaude, action)
		},
		"claude_upgrade.prepare": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeUpgradePrepare(backendUpgradeClaude, action)
		},
		"claude_restart.run": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeRestartRun(backendUpgradeClaude, action)
		},
		"claude_upgrade.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeUpgradeAction(backendUpgradeClaude, action, "claude_upgrade.confirm")
		},
		"claude_upgrade.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return newBackendUpgradeService(s.app).completeUpgradeAction(backendUpgradeClaude, action, "claude_upgrade.cancel")
		},
	}

}
