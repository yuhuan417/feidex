package app

import (
	"strings"

	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var workspaceCardActionHandlers = map[string]cardActionHandler{
	"workspace.use.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return newBindingService(s.app).completeBindingUse(action, actionSessionKey(action), strings.TrimSpace(action.Option))
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceUse(action, actionSessionKey(action), strings.TrimSpace(action.Option))
	},
	"workspace.use.existing": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return newBindingService(s.app).completeBindingUse(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceUseExisting(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.new": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return completeMenuCommand(s.app, action, actionSessionKey(action), "/workspace new", "menu.workspace")
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceNew(action, actionSessionKey(action))
	},
	"workspace.new.takeover": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceNewTakeover(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "target_dir"))
	},
	"workspace.clone": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return completeMenuCommand(s.app, action, actionSessionKey(action), "/workspace clone", "menu.workspace")
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceClone(action, actionSessionKey(action))
	},
	"workspace.worktree": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceWorktree(action, actionSessionKey(action))
	},
	"workspace.clone.use_existing": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return newBindingService(s.app).completeBindingUse(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceCloneUseExisting(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.clone.pickdir": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceClonePickDir(action)
	},
	"workspace.clone.refresh": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceCloneRefresh(action)
	},
	"workspace.clone.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceCloneCancel(action)
	},
	"workspace.clone.submit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if newBindingService(s.app).isGroupWorkspacePending(action, "workspace_clone") {
			return newBindingService(s.app).completeBindingWorkspaceCloneSubmit(action)
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceCloneSubmit(action)
	},
	"workspace.worktree.submit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if newBindingService(s.app).isGroupWorkspacePending(action, "workspace_worktree") {
			return newBindingService(s.app).completeBindingWorkspaceWorktreeSubmit(action)
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceWorktreeSubmit(action)
	},
	"workspace.worktree.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceWorktreeCancel(action)
	},
	"workspace.new.pickdir": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceNewPickDir(action)
	},
	"workspace.new.submit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if newBindingService(s.app).isGroupWorkspacePending(action, "workspace_new") {
			return newBindingService(s.app).completeBindingWorkspaceNewSubmit(action)
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceNewSubmit(action)
	},
	"workspace.sandbox.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return newBindingService(s.app).completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "sandbox")
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceSandboxMenu(action, actionSessionKey(action))
	},
	"workspace.policy.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return newBindingService(s.app).completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "policy")
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspacePolicyMenu(action, actionSessionKey(action))
	},
	"workspace.permission_mode.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return newBindingService(s.app).completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "permissions")
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteClaudeWorkspacePermissionMenu(action, actionSessionKey(action))
	},
	"workspace.delete.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "群聊中不能删除本机 workspace，请私聊该 Bot 使用 /workspace delete"}}, nil
		}
		return newWorkspaceConfigServiceInner(s.app).CompleteWorkspaceDeleteMenu(actionSessionKey(action))
	},
	"workspace.delete.prompt": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceConfigServiceInner(s.app).CompleteWorkspaceDeletePrompt(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.delete.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return newWorkspaceConfigServiceInner(s.app).CompleteWorkspaceDeleteConfirm(actionSessionKey(action), actionStringValue(action, "workspace_id"))
	},
	"workspace.sandbox.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return newBindingService(s.app).completeBindingSimpleOverride(action, actionSessionKey(action), "sandbox", actionStringValue(action, "sandbox_mode"))
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceSandboxSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "sandbox_mode"))
	},
	"workspace.policy.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return newBindingService(s.app).completeBindingSimpleOverride(action, actionSessionKey(action), "policy", actionStringValue(action, "approval_policy"))
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspacePolicySet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "approval_policy"))
	},
	"workspace.permission_mode.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return newBindingService(s.app).completeBindingSimpleOverride(action, actionSessionKey(action), "permissions", actionStringValue(action, "mode"))
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspacePermissionModeSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "mode"))
	},
	"workspace.multiagent.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return newBindingService(s.app).completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "multiagent")
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceMultiAgentMenu(action, actionSessionKey(action))
	},
	"workspace.multiagent.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		if groupBindingSessionScopeActive(s.app, actionSessionKey(action)) {
			return newBindingService(s.app).completeBindingSimpleOverride(action, actionSessionKey(action), "multiagent", actionStringValue(action, "multi_agent_mode"))
		}
		return newWorkspaceManagementServiceInner(s.app).CompleteWorkspaceMultiAgentSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "multi_agent_mode"))
	},
	"thread.sandbox.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return appthreadmenu.NewService(s.app).CompleteThreadSandboxMenu(action, actionSessionKey(action))
	},
	"thread.policy.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return appthreadmenu.NewService(s.app).CompleteThreadPolicyMenu(action, actionSessionKey(action))
	},
	"thread.permission_mode.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return appthreadmenu.NewService(s.app).CompleteClaudeSessionPermissionMenu(action, actionSessionKey(action))
	},
	"thread.sandbox.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return appthreadmenu.NewService(s.app).CompleteThreadSandboxSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "sandbox_mode"))
	},
	"thread.policy.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return appthreadmenu.NewService(s.app).CompleteThreadPolicySet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "approval_policy"))
	},
	"thread.multiagent.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return appthreadmenu.NewService(s.app).CompleteThreadMultiAgentMenu(action, actionSessionKey(action))
	},
	"thread.multiagent.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return appthreadmenu.NewService(s.app).CompleteThreadMultiAgentSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "multi_agent_mode"))
	},
	"thread.permission_mode.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return appthreadmenu.NewService(s.app).CompleteClaudeSessionPermissionModeSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "mode"))
	},
	"thread.resume.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return appthreadmenu.NewService(s.app).CompleteThreadResume(action, actionSessionKey(action), strings.TrimSpace(action.Option))
	},
	"path_picker.dropdown": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completePathPickerAction(s.app, action, "path_picker.dropdown")
	},
	"path_picker.up": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completePathPickerAction(s.app, action, "path_picker.up")
	},
	"path_picker.open": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completePathPickerAction(s.app, action, "path_picker.open")
	},
	"path_picker.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completePathPickerAction(s.app, action, "path_picker.select")
	},
	"path_picker.confirm": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completePathPickerAction(s.app, action, "path_picker.confirm")
	},
	"path_picker.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
		return completePathPickerAction(s.app, action, "path_picker.cancel")
	},
}
