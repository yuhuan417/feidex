package feishuapp

import (
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/domain/conversation"
	appruntime "feidex/internal/runtime"

	workspacecards "feidex/internal/adapter/feishu/workspace"
	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var workspaceGitClone = appworkspacecmd.GitClone

// buildWorkspaceManagementService takes the card presentation and conversation
// service as construction-time inputs; see buildWorkspaceConfigService.
func buildWorkspaceManagementService(a *App, presentation *workspacecards.Presentation, conversations *conversationapp.Service, scope BindingScope) *appworkspacecmd.ManagementService {
	if a == nil {
		return appworkspacecmd.NewManagementService(appworkspacecmd.ManagementDeps{})
	}

	st := a.State()
	replyRunner := newEffectRunner(a.runtimeOwner)
	frontendID := a.FrontendID()
	replyInThread := a.configView().replyInThreadEnabled()
	bindingScope := scope.scope
	threadMarker := liveThreadMarker{
		tracker: a.runtimeOwner.LiveThreads, state: st,
		announcement: a.bindings.AnnouncementQuery, refreshes: a.runtimeOwner.Announcements,
	}
	return appworkspacecmd.NewManagementService(appworkspacecmd.ManagementDeps{
		Dependencies: workspaceCommandApp(a, presentation),
		State:        workspaceStateDeps(st),
		SessionContext: appworkspacecmd.SessionContextDeps{
			SessionHasInFlight:     conversation.HasInFlightSubmission,
			ClearSessionLiveThread: threadMarker.tracker.Clear,
		},
		Threads: appworkspacecmd.ThreadDeps{
			EnsureWorkspaceThreadBinding: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return conversations.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
			},
			MarkSessionThreadLive:  threadMarker.MarkSessionThreadLive,
			ClearSessionLiveThread: threadMarker.tracker.Clear,
			StartWorkspaceThread: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return conversations.StartWorkspaceThread(sessionKey, sess, ws)
			},
		},
		Clone: appworkspacecmd.CloneDeps{
			SetCloneOp:   workspaceCloneSetOp(a.runtimeOwner),
			GetCloneOp:   workspaceCloneGetOp(a.runtimeOwner),
			ClearCloneOp: workspaceCloneClearOp(a.runtimeOwner),
			GitClone:     workspaceGitClone,
		},
		Backend: workspaceBackendConfigDeps(a.BackendDriver()),
		Actions: appworkspacecmd.ActionDeps{
			CompleteMenuCommand: func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
				return completeMenuCommand(a, action, sessionKey, rawCommand, parentAction)
			},
			ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
				return replyCommandActionResponseWith(replyRunner, frontendID, replyInThread, msg, resp)
			},
			CommandActionFromMessage: commandActionFromMessage,
			CommandMessageFromAction: func(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage {
				return commandMessageFromAction(bindingScope, action, sessionKey, rawCommand)
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
				return presentation.RenderWorkspaceNewCard(sessionKey, requestID, payload)
			},
			RenderCloneCard: func(sessionKey, requestID string, payload appworkspacecmd.ClonePayload) map[string]any {
				return presentation.RenderWorkspaceCloneCard(sessionKey, requestID, payload)
			},
			RenderClonePreparingCard: func(requestID string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return presentation.RenderWorkspaceClonePreparingCard(requestID, payload, parentDir, snapshot)
			},
			RenderCloneSuccessCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return presentation.RenderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir)
			},
			RenderWorktreeCard: func(sessionKey, requestID string, payload appworkspacecmd.WorktreePayload) map[string]any {
				return presentation.RenderWorkspaceWorktreeCard(sessionKey, requestID, payload)
			},
			RenderWorktreePreparingCard: func(requestID string, payload appworkspacecmd.WorktreePayload, plan *appworkspacecmd.WorktreePlan, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return presentation.RenderWorkspaceWorktreePreparingCard(requestID, payload, plan, snapshot)
			},
			RenderWorktreeSuccessCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return presentation.RenderWorkspaceWorktreeSuccessCard(sessionKey, workspaceID, targetDir)
			},
			RenderWorktreeManualHintCard: func(sessionKey, workspaceID, targetDir, errText string) map[string]any {
				return presentation.RenderWorkspaceWorktreeManualHintCard(sessionKey, workspaceID, targetDir, errText)
			},
			RenderWorktreeCanceledCard: func(sessionKey string, payload appworkspacecmd.WorktreePayload, plan *appworkspacecmd.WorktreePlan, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return presentation.RenderWorkspaceWorktreeCanceledCard(sessionKey, payload, plan, snapshot)
			},
			RenderSwitchExistingCard: func(sessionKey, workspaceID, targetDir, notice string) map[string]any {
				return presentation.RenderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, notice)
			},
			RenderCloneSwitchExistingCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return presentation.RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
			},
			RenderCloneManualHintCard: func(sessionKey, workspaceID, targetDir, errText string) map[string]any {
				return presentation.RenderWorkspaceCloneManualHintCard(sessionKey, workspaceID, targetDir, errText)
			},
			RenderCloneCanceledCard: func(sessionKey string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return presentation.RenderWorkspaceCloneCanceledCard(sessionKey, payload, parentDir, snapshot)
			},
			RenderMenuCard: func(sessionKey string) map[string]any {
				return presentation.RenderWorkspaceMenuCard(sessionKey)
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
