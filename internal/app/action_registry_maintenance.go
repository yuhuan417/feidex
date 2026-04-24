package app

import (
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var maintenanceCardActionHandlers = map[string]cardActionHandler{
	"upgrade.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newAppUpgradeService(s.app).completeUpgradeAction(action, "upgrade.confirm")
	},
	"upgrade.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newAppUpgradeService(s.app).completeUpgradeAction(action, "upgrade.cancel")
	},
	"upgrade.local.pick": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newAppUpgradeService(s.app).completeUpgradeLocalPick(action)
	},
	"upgrade.dev": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeUpgradeDev(action)
	},
	"codex_upgrade.refresh": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeCodexUpgradeRefresh(action)
	},
	"codex_upgrade.check": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeCodexUpgradeCheck(action)
	},
	"codex_upgrade.prepare": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeCodexUpgradePrepare(action)
	},
	"codex_restart.run": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeCodexRestartRun(action)
	},
	"codex_upgrade.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeCodexUpgradeAction(action, "codex_upgrade.confirm")
	},
	"codex_upgrade.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeCodexUpgradeAction(action, "codex_upgrade.cancel")
	},
	"claude_upgrade.refresh": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeClaudeUpgradeRefresh(action)
	},
	"claude_upgrade.check": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeClaudeUpgradeCheck(action)
	},
	"claude_upgrade.prepare": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeClaudeUpgradePrepare(action)
	},
	"claude_restart.run": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeClaudeRestartRun(action)
	},
	"claude_upgrade.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeClaudeUpgradeAction(action, "claude_upgrade.confirm")
	},
	"claude_upgrade.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeClaudeUpgradeAction(action, "claude_upgrade.cancel")
	},
}
