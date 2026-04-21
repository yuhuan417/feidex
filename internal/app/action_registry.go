package app

import (
	"strconv"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type cardActionHandler func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error)

// Card action handlers run on the Feishu callback ack path.
// Keep them fast: validate input, persist state, enqueue work, and return.
// Do not put clone/download/fetch/review/upgrade or other blocking workflows
// directly in these handlers.

var cardActionHandlers = map[string]cardActionHandler{
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
	"menu.codex_upgrade": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuCodexUpgrade(action)
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
	"menu.interrupt": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuInterrupt(action, actionSessionKey(action), actionStringValue(action, "turn_id"))
	},
	"menu.workspace": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuWorkspace(action, actionSessionKey(action))
	},
	"workspace.use.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceUse(action, actionSessionKey(action), strings.TrimSpace(action.Option))
	},
	"workspace.use.existing": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceUseExisting(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.new": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceNew(action, actionSessionKey(action))
	},
	"workspace.new.takeover": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceNewTakeover(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "target_dir"))
	},
	"workspace.clone": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceClone(action, actionSessionKey(action))
	},
	"workspace.clone.use_existing": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceCloneUseExisting(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.clone.pickdir": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceClonePickDir(action)
	},
	"workspace.clone.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceCloneCancel(action)
	},
	"workspace.clone.submit": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceCloneSubmit(action)
	},
	"workspace.new.pickdir": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceNewPickDir(action)
	},
	"workspace.new.submit": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceNewSubmit(action)
	},
	"workspace.sandbox.menu": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceSandboxMenu(action, actionSessionKey(action))
	},
	"workspace.policy.menu": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspacePolicyMenu(action, actionSessionKey(action))
	},
	"workspace.delete.menu": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceDeleteMenu(action, actionSessionKey(action))
	},
	"workspace.delete.prompt": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceDeletePrompt(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.delete.confirm": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceDeleteConfirm(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.sandbox.set": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspaceSandboxSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "sandbox_mode"))
	},
	"workspace.policy.set": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeWorkspacePolicySet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "approval_policy"))
	},
	"thread.sandbox.menu": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeThreadSandboxMenu(action, actionSessionKey(action))
	},
	"thread.policy.menu": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeThreadPolicyMenu(action, actionSessionKey(action))
	},
	"thread.sandbox.set": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeThreadSandboxSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "sandbox_mode"))
	},
	"thread.policy.set": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeThreadPolicySet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "approval_policy"))
	},
	"thread.resume.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeThreadResume(action, actionSessionKey(action), strings.TrimSpace(action.Option))
	},
	"history.page": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeHistoryPage(action, actionSessionKey(action), actionIntValue(action, "page"))
	},
	"history.detail": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeHistoryDetail(action, actionSessionKey(action), actionIntValue(action, "index"))
	},
	"history.detail.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		index, err := strconv.Atoi(strings.TrimSpace(action.Option))
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效 turn 选项"}}, nil
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
	"user_input.answer": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeUserInputAnswer(action)
	},
	"user_input.toggle_multi": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeUserInputMultiToggle(action)
	},
	"path_picker.dropdown": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completePathPickerAction(action, "path_picker.dropdown")
	},
	"path_picker.up": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completePathPickerAction(action, "path_picker.up")
	},
	"path_picker.open": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completePathPickerAction(action, "path_picker.open")
	},
	"path_picker.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completePathPickerAction(action, "path_picker.select")
	},
	"path_picker.confirm": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completePathPickerAction(action, "path_picker.confirm")
	},
	"path_picker.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completePathPickerAction(action, "path_picker.cancel")
	},
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
	"menu.claude_upgrade": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeMenuClaudeUpgrade(action)
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
	"approval.command.accept": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.command.accept")
	},
	"approval.command.accept_session": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.command.accept_session")
	},
	"approval.command.decline": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.command.decline")
	},
	"approval.command.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.command.cancel")
	},
	"approval.file.accept": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.file.accept")
	},
	"approval.file.accept_session": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.file.accept_session")
	},
	"approval.file.decline": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.file.decline")
	},
	"approval.file.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.file.cancel")
	},
	"approval.permissions.accept_turn": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.permissions.accept_turn")
	},
	"approval.permissions.accept_session": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeApprovalAction(action, "approval.permissions.accept_session")
	},
	"pending_form.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completePendingFormCancel(action)
	},
	"review.base.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeReviewBaseSelect(action)
	},
	"review.commit.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeReviewCommitSelect(action)
	},
	"review.form.submit": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeReviewFormSubmit(action)
	},
	"elicitation_url.accept": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeElicitationURLAction(action, "elicitation_url.accept")
	},
	"elicitation_url.decline": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeElicitationURLAction(action, "elicitation_url.decline")
	},
	"elicitation_url.cancel": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeElicitationURLAction(action, "elicitation_url.cancel")
	},
}

func resolvedCardActionName(action *feishu.CardAction) string {
	if action == nil {
		return ""
	}
	name, _ := action.ActionValue["action"].(string)
	if strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(action.Name)
}

func actionSessionKey(action *feishu.CardAction) string {
	return actionStringValue(action, "session_key")
}

func actionStringValue(action *feishu.CardAction, key string) string {
	if action == nil {
		return ""
	}
	value, _ := action.ActionValue[key].(string)
	return strings.TrimSpace(value)
}

func actionIntValue(action *feishu.CardAction, key string) int {
	if action == nil {
		return 0
	}
	switch value := action.ActionValue[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}
