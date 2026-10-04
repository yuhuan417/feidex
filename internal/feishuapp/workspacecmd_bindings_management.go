package feishuapp

import (
	"feidex/internal/domain/conversation"
	appruntime "feidex/internal/runtime"

	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var workspaceGitClone = appworkspacecmd.GitClone

func buildWorkspaceManagementService(a *App) *appworkspacecmd.ManagementService {
	if a == nil {
		return appworkspacecmd.NewManagementService(appworkspacecmd.ManagementDeps{})
	}

	st := a.State()
	bcfg := a.bindings.BackendConfiguration
	return appworkspacecmd.NewManagementService(appworkspacecmd.ManagementDeps{
		Dependencies: workspaceCommandApp(a),
		State:        workspaceStateDeps(st),
		SessionContext: appworkspacecmd.SessionContextDeps{
			SessionHasInFlight:     conversation.HasInFlightSubmission,
			ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },
		},
		Threads: appworkspacecmd.ThreadDeps{
			EnsureWorkspaceThreadBinding: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return a.bindings.Conversations.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
			},
			MarkSessionThreadLive:  func(sessionKey, threadID string) { markSessionThreadLive(a, sessionKey, threadID) },
			ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },
			StartWorkspaceThread: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return a.bindings.Conversations.StartWorkspaceThread(sessionKey, sess, ws)
			},
		},
		Clone: appworkspacecmd.CloneDeps{
			SetCloneOp:   workspaceCloneSetOp(a.runtimeOwner),
			GetCloneOp:   workspaceCloneGetOp(a.runtimeOwner),
			ClearCloneOp: workspaceCloneClearOp(a.runtimeOwner),
			GitClone:     workspaceGitClone,
		},
		Backend: appworkspacecmd.BackendConfigDeps{
			BackendWorkspaceSwitchBindingNotice:        bcfg.BackendWorkspaceSwitchBindingNotice,
			BackendWorkspaceSwitchBindingFailureNotice: bcfg.BackendWorkspaceSwitchBindingFailureNotice,
			BackendWorkspaceSwitchInFlightNotice:       bcfg.BackendWorkspaceSwitchInFlightNotice,
			BackendWorkspaceCommandUsage:               bcfg.BackendWorkspaceCommandUsage,
			BackendWorkspacePermissionCommand:          bcfg.HandleBackendWorkspacePermissionCommand,
		},
		Actions: appworkspacecmd.ActionDeps{
			CompleteMenuCommand: func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
				return completeMenuCommand(a, action, sessionKey, rawCommand, parentAction)
			},
			ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
				return replyCommandActionResponse(a, msg, resp)
			},
			CommandActionFromMessage: commandActionFromMessage,
			CommandMessageFromAction: func(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage {
				return commandMessageFromAction(a, action, sessionKey, rawCommand)
			},
		},
		Formatting: appworkspacecmd.FormattingDeps{
			FormatMenuBody: menuCardBody,
		},
		Async: appworkspacecmd.AsyncDeps{
			RunAsync: func(fn func()) { runAsync(a, fn) },
		},
		Render: appworkspacecmd.ManagementRenderDeps{
			RenderNewCard: func(sessionKey, requestID string, payload appworkspacecmd.NewPayload) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceNewCard(sessionKey, requestID, payload)
			},
			RenderCloneCard: func(sessionKey, requestID string, payload appworkspacecmd.ClonePayload) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceCloneCard(sessionKey, requestID, payload)
			},
			RenderClonePreparingCard: func(requestID string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceClonePreparingCard(requestID, payload, parentDir, snapshot)
			},
			RenderCloneSuccessCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir)
			},
			RenderWorktreeCard: func(sessionKey, requestID string, payload appworkspacecmd.WorktreePayload) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceWorktreeCard(sessionKey, requestID, payload)
			},
			RenderWorktreePreparingCard: func(requestID string, payload appworkspacecmd.WorktreePayload, plan *appworkspacecmd.WorktreePlan, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceWorktreePreparingCard(requestID, payload, plan, snapshot)
			},
			RenderWorktreeSuccessCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceWorktreeSuccessCard(sessionKey, workspaceID, targetDir)
			},
			RenderWorktreeManualHintCard: func(sessionKey, workspaceID, targetDir, errText string) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceWorktreeManualHintCard(sessionKey, workspaceID, targetDir, errText)
			},
			RenderWorktreeCanceledCard: func(sessionKey string, payload appworkspacecmd.WorktreePayload, plan *appworkspacecmd.WorktreePlan, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceWorktreeCanceledCard(sessionKey, payload, plan, snapshot)
			},
			RenderSwitchExistingCard: func(sessionKey, workspaceID, targetDir, notice string) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, notice)
			},
			RenderCloneSwitchExistingCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
			},
			RenderCloneManualHintCard: func(sessionKey, workspaceID, targetDir, errText string) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceCloneManualHintCard(sessionKey, workspaceID, targetDir, errText)
			},
			RenderCloneCanceledCard: func(sessionKey string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceCloneCanceledCard(sessionKey, payload, parentDir, snapshot)
			},
			RenderMenuCard: func(sessionKey string) map[string]any {
				return a.bindings.WorkspacePresentation.RenderWorkspaceMenuCard(sessionKey)
			},
		},
	})
}

func workspaceCloneSetOp(runtimeowner *appruntime.FrontendOwner) func(string, *appworkspacecmd.CloneOperation) {
	return runtimeowner.WorkspaceCloneOps.Set
}
func workspaceCloneGetOp(runtimeowner *appruntime.FrontendOwner) func(string) *appworkspacecmd.CloneOperation {
	return runtimeowner.WorkspaceCloneOps.Get
}
func workspaceCloneClearOp(runtimeowner *appruntime.FrontendOwner) func(string) {
	return runtimeowner.WorkspaceCloneOps.Clear
}
