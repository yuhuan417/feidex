package app

import (
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var maintenanceCardActionHandlers = map[string]cardActionHandler{
	"upgrade.confirm": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeUpgradeAction(action, "upgrade.confirm")
	},
	"upgrade.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeUpgradeAction(action, "upgrade.cancel")
	},
	"upgrade.local.pick": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeUpgradeLocalPick(action)
	},
	"upgrade.dev": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeUpgradeDev(action)
	},
	"codex_upgrade.refresh": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeCodexUpgradeRefresh(action)
	},
	"codex_upgrade.check": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeCodexUpgradeCheck(action)
	},
	"codex_upgrade.prepare": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeCodexUpgradePrepare(action)
	},
	"codex_restart.run": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeCodexRestartRun(action)
	},
	"codex_upgrade.confirm": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeCodexUpgradeAction(action, "codex_upgrade.confirm")
	},
	"codex_upgrade.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeCodexUpgradeAction(action, "codex_upgrade.cancel")
	},
	"claude_upgrade.refresh": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeClaudeUpgradeRefresh(action)
	},
	"claude_upgrade.check": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeClaudeUpgradeCheck(action)
	},
	"claude_upgrade.prepare": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeClaudeUpgradePrepare(action)
	},
	"claude_restart.run": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeClaudeRestartRun(action)
	},
	"claude_upgrade.confirm": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeClaudeUpgradeAction(action, "claude_upgrade.confirm")
	},
	"claude_upgrade.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeClaudeUpgradeAction(action, "claude_upgrade.cancel")
	},
}
