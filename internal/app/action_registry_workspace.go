package app

import (
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var workspaceCardActionHandlers = map[string]cardActionHandler{
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
	"workspace.permission_mode.menu": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeClaudeWorkspacePermissionMenu(action, actionSessionKey(action))
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
	"workspace.permission_mode.set": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeClaudeWorkspacePermissionModeSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "mode"))
	},
	"thread.sandbox.menu": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeThreadSandboxMenu(action, actionSessionKey(action))
	},
	"thread.policy.menu": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeThreadPolicyMenu(action, actionSessionKey(action))
	},
	"thread.permission_mode.menu": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeClaudeSessionPermissionMenu(action, actionSessionKey(action))
	},
	"thread.sandbox.set": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeThreadSandboxSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "sandbox_mode"))
	},
	"thread.policy.set": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeThreadPolicySet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "approval_policy"))
	},
	"thread.permission_mode.set": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeClaudeSessionPermissionModeSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "mode"))
	},
	"thread.resume.select": func(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return a.completeThreadResume(action, actionSessionKey(action), strings.TrimSpace(action.Option))
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
}
