package feishuapp

import (
	"strings"

	appthreadmenu "feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/adapter/feishu/workspacecmd"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type WorkspaceCardActionInputs struct {
	BindingCommands        bindingService
	WorkspaceManagement    *workspacecmd.ManagementService
	WorkspaceConfiguration *workspacecmd.ConfigService
	ThreadMenu             *appthreadmenu.Service
	CompleteMenuCommand    func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
}

func workspaceCardActionHandlers(inputs WorkspaceCardActionInputs) map[string]cardActionPortHandler {
	return map[string]cardActionPortHandler{
		"menu.workspace": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/workspace", "menu.root")
		},
		"workspace.use.select": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.BindingCommands.completeBindingUse(action, actionSessionKey(action), strings.TrimSpace(action.Option))
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceUse(action, actionSessionKey(action), strings.TrimSpace(action.Option))
		},
		"workspace.use.existing": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.BindingCommands.completeBindingUse(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceUseExisting(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
		},
		"workspace.binding.unbind": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.BindingCommands.completeBindingWorkspaceUnbind(action, actionSessionKey(action))
		},
		"workspace.new": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/workspace new", "menu.workspace")
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceNew(action, actionSessionKey(action))
		},
		"workspace.new.takeover": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.WorkspaceManagement.CompleteWorkspaceNewTakeover(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "target_dir"))
		},
		"workspace.clone": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.CompleteMenuCommand(action, actionSessionKey(action), "/workspace clone", "menu.workspace")
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceClone(action, actionSessionKey(action))
		},
		"workspace.worktree": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.WorkspaceManagement.CompleteWorkspaceWorktree(action, actionSessionKey(action))
		},
		"workspace.clone.use_existing": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.BindingCommands.completeBindingUse(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceCloneUseExisting(action, actionSessionKey(action), actionStringValue(action, "workspace_id"))
		},
		"workspace.clone.pickdir": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.WorkspaceManagement.CompleteWorkspaceClonePickDir(action)
		},
		"workspace.clone.refresh": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.WorkspaceManagement.CompleteWorkspaceCloneRefresh(action)
		},
		"workspace.clone.cancel": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.WorkspaceManagement.CompleteWorkspaceCloneCancel(action)
		},
		"workspace.clone.submit": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if inputs.BindingCommands.isGroupWorkspacePending(action, "workspace_clone") {
				return inputs.BindingCommands.completeBindingWorkspaceCloneSubmit(action)
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceCloneSubmit(action)
		},
		"workspace.worktree.submit": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if inputs.BindingCommands.isGroupWorkspacePending(action, "workspace_worktree") {
				return inputs.BindingCommands.completeBindingWorkspaceWorktreeSubmit(action)
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceWorktreeSubmit(action)
		},
		"workspace.worktree.cancel": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.WorkspaceManagement.CompleteWorkspaceWorktreeCancel(action)
		},
		"workspace.new.pickdir": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.WorkspaceManagement.CompleteWorkspaceNewPickDir(action)
		},
		"workspace.new.submit": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if inputs.BindingCommands.isGroupWorkspacePending(action, "workspace_new") {
				return inputs.BindingCommands.completeBindingWorkspaceNewSubmit(action)
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceNewSubmit(action)
		},
		"workspace.sandbox.menu": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.BindingCommands.completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "sandbox")
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceSandboxMenu(action, actionSessionKey(action))
		},
		"workspace.policy.menu": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.BindingCommands.completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "policy")
			}
			return inputs.WorkspaceManagement.CompleteWorkspacePolicyMenu(action, actionSessionKey(action))
		},
		"workspace.permission_mode.menu": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.BindingCommands.completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "permissions")
			}
			return inputs.WorkspaceManagement.CompleteClaudeWorkspacePermissionMenu(action, actionSessionKey(action))
		},
		"workspace.delete.menu": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "群聊中不能删除本机 workspace，请私聊该 Bot 使用 /workspace delete"}}, nil
			}
			return inputs.WorkspaceConfiguration.CompleteWorkspaceDeleteMenu(actionSessionKey(action))
		},
		"workspace.sandbox.set": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.BindingCommands.completeBindingSimpleOverride(action, actionSessionKey(action), "sandbox", actionStringValue(action, "sandbox_mode"))
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceSandboxSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "sandbox_mode"))
		},
		"workspace.policy.set": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.BindingCommands.completeBindingSimpleOverride(action, actionSessionKey(action), "policy", actionStringValue(action, "approval_policy"))
			}
			return inputs.WorkspaceManagement.CompleteWorkspacePolicySet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "approval_policy"))
		},
		"workspace.permission_mode.set": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.BindingCommands.completeBindingSimpleOverride(action, actionSessionKey(action), "permissions", actionStringValue(action, "mode"))
			}
			return inputs.WorkspaceManagement.CompleteWorkspacePermissionModeSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "mode"))
		},
		"workspace.multiagent.menu": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.BindingCommands.completeBindingWorkspaceSettingMenu(action, actionSessionKey(action), "multiagent")
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceMultiAgentMenu(action, actionSessionKey(action))
		},
		"workspace.multiagent.set": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			if groupBindingSessionScopeActive(inputs.BindingCommands.scope, actionSessionKey(action)) {
				return inputs.BindingCommands.completeBindingSimpleOverride(action, actionSessionKey(action), "multiagent", actionStringValue(action, "multi_agent_mode"))
			}
			return inputs.WorkspaceManagement.CompleteWorkspaceMultiAgentSet(action, actionSessionKey(action), actionStringValue(action, "workspace_id"), actionStringValue(action, "multi_agent_mode"))
		},
		"thread.sandbox.menu": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.ThreadMenu.CompleteThreadSandboxMenu(action, actionSessionKey(action))
		},
		"thread.policy.menu": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.ThreadMenu.CompleteThreadPolicyMenu(action, actionSessionKey(action))
		},
		"thread.permission_mode.menu": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.ThreadMenu.CompleteClaudeSessionPermissionMenu(action, actionSessionKey(action))
		},
		"thread.sandbox.set": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.ThreadMenu.CompleteThreadSandboxSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "sandbox_mode"))
		},
		"thread.policy.set": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.ThreadMenu.CompleteThreadPolicySet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "approval_policy"))
		},
		"thread.multiagent.menu": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.ThreadMenu.CompleteThreadMultiAgentMenu(action, actionSessionKey(action))
		},
		"thread.multiagent.set": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.ThreadMenu.CompleteThreadMultiAgentSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "multi_agent_mode"))
		},
		"thread.permission_mode.set": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.ThreadMenu.CompleteClaudeSessionPermissionModeSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "mode"))
		},
		"thread.resume.select": func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
			return inputs.ThreadMenu.CompleteThreadResume(action, actionSessionKey(action), strings.TrimSpace(action.Option))
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
