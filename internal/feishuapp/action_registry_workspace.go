package feishuapp

import (
	"strings"

	"feidex/internal/adapter/feishu/workspacecmd"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func workspaceCardActionHandlers() map[string]cardActionHandler {
	return map[string]cardActionHandler{
		"workspace.use.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return s.app.bindings.BindingCommands.completeBindingUse(action, actionSessionKey(action), strings.TrimSpace(action.Option))
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceUse(action, actionSessionKey(action), strings.TrimSpace(action.Option))
		},
		"workspace.use.existing": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return s.app.bindings.BindingCommands.completeBindingUse(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceUseExisting(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
		},
		"workspace.binding.unbind": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.BindingCommands.completeBindingWorkspaceUnbind(action, actionSessionKey(action))
		},
		"workspace.new": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return completeMenuCommand(s.app, action, actionSessionKey(action), "/workspace new", "menu.workspace")
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceNew(action, actionSessionKey(action))
		},
		"workspace.new.takeover": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceNewTakeover(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "target_dir"))
		},
		"workspace.clone": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return completeMenuCommand(s.app, action, actionSessionKey(action), "/workspace clone", "menu.workspace")
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceClone(action, actionSessionKey(action))
		},
		"workspace.worktree": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceWorktree(action, actionSessionKey(action))
		},
		"workspace.clone.use_existing": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return s.app.bindings.BindingCommands.completeBindingUse(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceCloneUseExisting(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
		},
		"workspace.clone.pickdir": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceClonePickDir(action)
		},
		"workspace.clone.refresh": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceCloneRefresh(action)
		},
		"workspace.clone.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceCloneCancel(action)
		},
		"workspace.clone.submit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if s.app.bindings.BindingCommands.isGroupWorkspacePending(action, "workspace_clone") {
				return s.app.bindings.BindingCommands.completeBindingWorkspaceCloneSubmit(action)
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceCloneSubmit(action)
		},
		"workspace.worktree.submit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if s.app.bindings.BindingCommands.isGroupWorkspacePending(action, "workspace_worktree") {
				return s.app.bindings.BindingCommands.completeBindingWorkspaceWorktreeSubmit(action)
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceWorktreeSubmit(action)
		},
		"workspace.worktree.cancel": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceWorktreeCancel(action)
		},
		"workspace.new.pickdir": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceNewPickDir(action)
		},
		"workspace.new.submit": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if s.app.bindings.BindingCommands.isGroupWorkspacePending(action, "workspace_new") {
				return s.app.bindings.BindingCommands.completeBindingWorkspaceNewSubmit(action)
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceNewSubmit(action)
		},
		"workspace.sandbox.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return s.app.bindings.BindingCommands.completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "sandbox")
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceSandboxMenu(action, actionSessionKey(action))
		},
		"workspace.policy.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return s.app.bindings.BindingCommands.completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "policy")
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspacePolicyMenu(action, actionSessionKey(action))
		},
		"workspace.permission_mode.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return s.app.bindings.BindingCommands.completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "permissions")
			}
			return s.app.bindings.WorkspaceManagement.CompleteClaudeWorkspacePermissionMenu(action, actionSessionKey(action))
		},
		"workspace.delete.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "群聊中不能删除本机 workspace，请私聊该 Bot 使用 /workspace delete"}}, nil
			}
			return s.app.bindings.WorkspaceConfiguration.CompleteWorkspaceDeleteMenu(actionSessionKey(action))
		},
		"workspace.sandbox.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return s.app.bindings.BindingCommands.completeBindingSimpleOverride(action, actionSessionKey(action), "sandbox", actionStringValue(action, "sandbox_mode"))
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceSandboxSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "sandbox_mode"))
		},
		"workspace.policy.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return s.app.bindings.BindingCommands.completeBindingSimpleOverride(action, actionSessionKey(action), "policy", actionStringValue(action, "approval_policy"))
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspacePolicySet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "approval_policy"))
		},
		"workspace.permission_mode.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return s.app.bindings.BindingCommands.completeBindingSimpleOverride(action, actionSessionKey(action), "permissions", actionStringValue(action, "mode"))
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspacePermissionModeSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "mode"))
		},
		"workspace.multiagent.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return s.app.bindings.BindingCommands.completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "multiagent")
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceMultiAgentMenu(action, actionSessionKey(action))
		},
		"workspace.multiagent.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(s.app.bindings.BindingCommands.scope, actionSessionKey(action)) {
				return s.app.bindings.BindingCommands.completeBindingSimpleOverride(action, actionSessionKey(action), "multiagent", actionStringValue(action, "multi_agent_mode"))
			}
			return s.app.bindings.WorkspaceManagement.CompleteWorkspaceMultiAgentSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "multi_agent_mode"))
		},
		"thread.sandbox.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.ThreadMenu.CompleteThreadSandboxMenu(action, actionSessionKey(action))
		},
		"thread.policy.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.ThreadMenu.CompleteThreadPolicyMenu(action, actionSessionKey(action))
		},
		"thread.permission_mode.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.ThreadMenu.CompleteClaudeSessionPermissionMenu(action, actionSessionKey(action))
		},
		"thread.sandbox.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.ThreadMenu.CompleteThreadSandboxSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "sandbox_mode"))
		},
		"thread.policy.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.ThreadMenu.CompleteThreadPolicySet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "approval_policy"))
		},
		"thread.multiagent.menu": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.ThreadMenu.CompleteThreadMultiAgentMenu(action, actionSessionKey(action))
		},
		"thread.multiagent.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.ThreadMenu.CompleteThreadMultiAgentSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "multi_agent_mode"))
		},
		"thread.permission_mode.set": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.ThreadMenu.CompleteClaudeSessionPermissionModeSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "mode"))
		},
		"thread.resume.select": func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return s.app.bindings.ThreadMenu.CompleteThreadResume(action, actionSessionKey(action), strings.TrimSpace(action.Option))
		},
	}

}

func workspaceDeletePortCardActionHandlers(service workspacecmd.WorkspaceDeleteActions) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		"workspace.delete.prompt": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return service.CompleteWorkspaceDeletePrompt(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
		},
		"workspace.delete.confirm": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return service.CompleteWorkspaceDeleteConfirm(actionSessionKey(action), actionStringValue(action, "workspace_id"))
		},
	}
}
