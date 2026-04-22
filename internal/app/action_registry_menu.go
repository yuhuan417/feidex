package app

import (
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var menuCardActionHandlers = map[string]cardActionHandler{
	"menu.root": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuRoot(action, actionSessionKey(action))
	},
	"menu.tools": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuTools(action, actionSessionKey(action))
	},
	"menu.group.model": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuGroupModel(action, actionSessionKey(action))
	},
	"menu.group.system": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuGroupSystem(action, actionSessionKey(action))
	},
	"menu.thread": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuThread(action, actionSessionKey(action))
	},
	"menu.new": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuNew(action, actionSessionKey(action))
	},
	"menu.download": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuDownload(action, actionSessionKey(action))
	},
	"menu.quiet": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuQuiet(action, actionSessionKey(action))
	},
	"menu.compact": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuCompact(action, actionSessionKey(action))
	},
	"menu.review": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuReview(action, actionSessionKey(action))
	},
	"menu.review.uncommitted": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuCommand(action, actionSessionKey(action), "/review", "menu.review")
	},
	"menu.review.base": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuCommand(action, actionSessionKey(action), "/review base", "menu.review")
	},
	"menu.review.commit": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuCommand(action, actionSessionKey(action), "/review commit", "menu.review")
	},
	"menu.review.custom": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuCommand(action, actionSessionKey(action), "/review custom", "menu.review")
	},
	"menu.fork": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuFork(action, actionSessionKey(action))
	},
	"menu.usage": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuUsage(action, actionSessionKey(action))
	},
	"menu.skills": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuSkills(action, actionSessionKey(action))
	},
	"menu.model": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuModel(action, actionSessionKey(action))
	},
	"menu.fast": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuFast(action, actionSessionKey(action))
	},
	"menu.status": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuStatus(action, actionSessionKey(action))
	},
	"menu.debug": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuDebug(action, actionSessionKey(action))
	},
	"menu.debug.logs": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuDebugLogs(action, actionSessionKey(action))
	},
	"menu.backend": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuBackend(action, actionSessionKey(action))
	},
	"menu.help": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuHelp(action, actionSessionKey(action))
	},
	"menu.history": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuHistory(action, actionSessionKey(action))
	},
	"menu.interrupt": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuInterrupt(action, actionSessionKey(action), actionStringValue(action, "turn_id"))
	},
	"menu.workspace": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuWorkspace(action, actionSessionKey(action))
	},
	"menu.codex_upgrade": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuCodexUpgrade(action)
	},
	"menu.claude_upgrade": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuClaudeUpgrade(action)
	},
	"menu.upgrade": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuUpgrade(action)
	},
	"quiet.set": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeQuietSet(action, config.QuietMode(actionStringValue(action, "mode")))
	},
	"service_tier.set": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeServiceTierSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "service_tier"))
	},
	"model.config.set_model": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeGlobalModelSet(action, actionStringValue(action, "model_id"))
	},
	"model.config.select_model": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		modelID := strings.TrimSpace(action.Option)
		if modelID == modelConfigDefaultOptionValue {
			modelID = ""
		}
		return a.completeGlobalModelSet(action, modelID)
	},
	"model.config.set_effort": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeGlobalReasoningEffortSet(action, actionStringValue(action, "reasoning_effort"))
	},
	"model.config.select_effort": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		reasoningEffort := strings.TrimSpace(action.Option)
		if reasoningEffort == modelConfigDefaultOptionValue {
			reasoningEffort = ""
		}
		return a.completeGlobalReasoningEffortSet(action, reasoningEffort)
	},
	"history.page": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeHistoryPage(action, actionSessionKey(action), actionIntValue(action, "page"))
	},
	"history.detail": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeHistoryDetail(action, actionSessionKey(action), actionIntValue(action, "index"))
	},
	"history.detail.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		errResp, index, ok := actionIndexOption(action, "未收到有效 turn 选项")
		if !ok {
			return errResp, nil
		}
		return a.completeHistoryDetail(action, actionSessionKey(action), index)
	},
	"skills.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeSkillsSelect(action, actionSessionKey(action), strings.TrimSpace(action.Option))
	},
	"skills.reload": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeSkillsReload(action, actionSessionKey(action))
	},
	"backend.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeBackendSelect(action, actionSessionKey(action), actionStringValue(action, "backend"))
	},
}
