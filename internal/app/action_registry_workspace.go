package app

import (
	"strings"

	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var workspaceCardActionHandlers = map[string]cardActionHandler{
	"workspace.use.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceUse(action, actionSessionKey(action), strings.TrimSpace(action.Option))
	},
	"workspace.use.existing": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceUseExisting(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.new": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceNew(action, actionSessionKey(action))
	},
	"workspace.new.takeover": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceNewTakeover(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "target_dir"))
	},
	"workspace.clone": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceClone(action, actionSessionKey(action))
	},
	"workspace.clone.use_existing": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceCloneUseExisting(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.clone.pickdir": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceClonePickDir(action)
	},
	"workspace.clone.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceCloneCancel(action)
	},
	"workspace.clone.submit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceCloneSubmit(action)
	},
	"workspace.new.pickdir": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceNewPickDir(action)
	},
	"workspace.new.submit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceNewSubmit(action)
	},
	"workspace.sandbox.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceSandboxMenu(action, actionSessionKey(action))
	},
	"workspace.policy.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspacePolicyMenu(action, actionSessionKey(action))
	},
	"workspace.permission_mode.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeClaudeWorkspacePermissionMenu(action, actionSessionKey(action))
	},
	"workspace.delete.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceDeleteMenu(action, actionSessionKey(action))
	},
	"workspace.delete.prompt": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceDeletePrompt(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.delete.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceDeleteConfirm(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.sandbox.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspaceSandboxSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "sandbox_mode"))
	},
	"workspace.policy.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeWorkspacePolicySet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "approval_policy"))
	},
	"workspace.permission_mode.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completeClaudeWorkspacePermissionModeSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "mode"))
	},
	"thread.sandbox.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newThreadService(s.app).completeThreadSandboxMenu(action, actionSessionKey(action))
	},
	"thread.policy.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newThreadService(s.app).completeThreadPolicyMenu(action, actionSessionKey(action))
	},
	"thread.permission_mode.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newThreadService(s.app).completeClaudeSessionPermissionMenu(action, actionSessionKey(action))
	},
	"thread.sandbox.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newThreadService(s.app).completeThreadSandboxSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "sandbox_mode"))
	},
	"thread.policy.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newThreadService(s.app).completeThreadPolicySet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "approval_policy"))
	},
	"thread.permission_mode.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newThreadService(s.app).completeClaudeSessionPermissionModeSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "mode"))
	},
	"thread.resume.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newThreadService(s.app).completeThreadResume(action, actionSessionKey(action), strings.TrimSpace(action.Option))
	},
	"path_picker.dropdown": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completePathPickerAction(action, "path_picker.dropdown")
	},
	"path_picker.up": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completePathPickerAction(action, "path_picker.up")
	},
	"path_picker.open": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completePathPickerAction(action, "path_picker.open")
	},
	"path_picker.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completePathPickerAction(action, "path_picker.select")
	},
	"path_picker.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completePathPickerAction(action, "path_picker.confirm")
	},
	"path_picker.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceService(s.app).completePathPickerAction(action, "path_picker.cancel")
	},
}
