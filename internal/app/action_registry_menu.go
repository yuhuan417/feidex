package app

import (
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var menuCardActionHandlers = map[string]cardActionHandler{
	"menu.root": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuRoot(action, actionSessionKey(action))
	},
	"menu.tools": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuTools(action, actionSessionKey(action))
	},
	"menu.group.model": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuGroupModel(action, actionSessionKey(action))
	},
	"menu.group.system": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuGroupSystem(action, actionSessionKey(action))
	},
	"menu.group.backend": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeMenuBackend(action, actionSessionKey(action))
	},
	"menu.thread": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newThreadActionService(s.app).completeMenuThread(action, actionSessionKey(action))
	},
	"menu.new": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newThreadActionService(s.app).completeMenuNew(action, actionSessionKey(action))
	},
	"menu.download": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newConversationWorkflowService(s.app).completeMenuDownload(action, actionSessionKey(action))
	},
	"menu.quiet": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuQuiet(action, actionSessionKey(action))
	},
	"menu.compact": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuCompact(action, actionSessionKey(action))
	},
	"menu.review": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuReview(action, actionSessionKey(action))
	},
	"menu.review.uncommitted": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newConversationWorkflowService(s.app).completeMenuReviewUncommitted(action, actionSessionKey(action))
	},
	"menu.review.base": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newConversationWorkflowService(s.app).completeMenuReviewBase(action, actionSessionKey(action))
	},
	"menu.review.commit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newConversationWorkflowService(s.app).completeMenuReviewCommit(action, actionSessionKey(action))
	},
	"menu.review.custom": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeMenuCommand(action, actionSessionKey(action), "/review custom", "menu.review")
	},
	"menu.fork": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newConversationWorkflowService(s.app).completeMenuFork(action, actionSessionKey(action))
	},
	"menu.usage": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuUsage(action, actionSessionKey(action))
	},
	"menu.skills": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuSkills(action, actionSessionKey(action))
	},
	"menu.model": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuModel(action, actionSessionKey(action))
	},
	"menu.fast": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuFast(action, actionSessionKey(action))
	},
	"menu.status": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuStatus(action, actionSessionKey(action))
	},
	"menu.debug": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newDebugService(s.app).completeMenuDebug(action, actionSessionKey(action))
	},
	"menu.debug.logs": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newDebugService(s.app).completeMenuDebugLogs(action, actionSessionKey(action))
	},
	"menu.backend": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuBackendSwitch(action, actionSessionKey(action))
	},
	"menu.backend.switch": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuBackendSwitch(action, actionSessionKey(action))
	},
	"menu.auto_retry": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeMenuCommand(action, actionSessionKey(action), "/backend retry", "menu.group.backend")
	},
	"menu.help": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuHelp(action, actionSessionKey(action))
	},
	"menu.history": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuHistory(action, actionSessionKey(action))
	},
	"menu.interrupt": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newThreadActionService(s.app).completeMenuInterrupt(action, actionSessionKey(action), actionStringValue(action, "turn_id"))
	},
	"menu.workspace": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeMenuWorkspace(action, actionSessionKey(action))
	},
	"menu.codex_upgrade": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeMenuCodexUpgrade(action)
	},
	"menu.claude_upgrade": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeMenuClaudeUpgrade(action)
	},
	"menu.upgrade": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeMenuUpgrade(action)
	},
	"quiet.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeQuietSet(action, config.QuietMode(actionStringValue(action, "mode")))
	},
	"service_tier.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeServiceTierSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "service_tier"))
	},
	"model.config.set_model": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newBackendConfigurationService(s.app).completeGlobalModelSet(action, actionStringValue(action, "model_id"))
	},
	"model.config.select_model": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		modelID := strings.TrimSpace(action.Option)
		if modelID == modelConfigDefaultOptionValue {
			modelID = ""
		}
		return newBackendConfigurationService(s.app).completeGlobalModelSet(action, modelID)
	},
	"model.config.set_effort": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newBackendConfigurationService(s.app).completeGlobalReasoningEffortSet(action, actionStringValue(action, "reasoning_effort"))
	},
	"model.config.select_effort": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		reasoningEffort := strings.TrimSpace(action.Option)
		if reasoningEffort == modelConfigDefaultOptionValue {
			reasoningEffort = ""
		}
		return newBackendConfigurationService(s.app).completeGlobalReasoningEffortSet(action, reasoningEffort)
	},
	"history.page": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeHistoryPage(action, actionSessionKey(action), actionIntValue(action, "page"))
	},
	"history.detail": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newMenuActionService(s.app).completeHistoryDetail(action, actionSessionKey(action), actionIntValue(action, "index"))
	},
	"history.detail.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		errResp, index, ok := actionIndexOption(action, "未收到有效 turn 选项")
		if !ok {
			return errResp, nil
		}
		return newMenuActionService(s.app).completeHistoryDetail(action, actionSessionKey(action), index)
	},
	"skills.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newSkillsService(s.app).completeSkillsSelect(action, actionSessionKey(action), strings.TrimSpace(action.Option))
	},
	"skills.reload": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newSkillsService(s.app).completeSkillsReload(action, actionSessionKey(action))
	},
	"backend.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return s.app.completeBackendSelect(action, actionSessionKey(action), actionStringValue(action, "backend"))
	},
	"auto_retry.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newAutoRetryService(s.app).completeAutoRetrySet(action, strings.EqualFold(actionStringValue(action, "enabled"), "on"))
	},
}
