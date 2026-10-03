package feishuapp

import (
	"feidex/internal/domain/conversation"
	"strings"

	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var workspaceGitClone = appworkspacecmd.GitClone

func newWorkspaceCloneTracker() *appworkspacecmd.CloneTracker {
	return appworkspacecmd.NewCloneTracker()
}

func newWorkspaceManagementService(a *App) *appworkspacecmd.ManagementService {
	return compositionService(a, "workspaceManage", func() *appworkspacecmd.ManagementService {
		return buildWorkspaceManagementService(a)
	})
}

func buildWorkspaceManagementService(a *App) *appworkspacecmd.ManagementService {
	if a == nil {
		return appworkspacecmd.NewManagementService(appworkspacecmd.ManagementDeps{})
	}

	st := a.State()
	bcfg := newBackendConfigurationService(a)
	return appworkspacecmd.NewManagementService(appworkspacecmd.ManagementDeps{
		Dependencies: workspaceCommandApp(a),
		State:        workspaceStateDeps(st),
		SessionContext: appworkspacecmd.SessionContextDeps{
			SessionHasInFlight:     conversation.HasInFlightSubmission,
			SetSessionThreadCtx:    conversation.SetThreadContext,
			SessionResetActiveOps:  conversation.ResetActiveOperations,
			ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },
		},
		Threads: appworkspacecmd.ThreadDeps{
			EnsureWorkspaceThreadBinding: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return newConversationService(a).EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
			},
			MarkSessionThreadLive:  func(sessionKey, threadID string) { markSessionThreadLive(a, sessionKey, threadID) },
			ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },
			StartWorkspaceThread: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return newConversationService(a).StartWorkspaceThread(sessionKey, sess, ws)
			},
		},
		Clone: appworkspacecmd.CloneDeps{
			SetCloneOp:   workspaceCloneSetOp(a),
			GetCloneOp:   workspaceCloneGetOp(a),
			ClearCloneOp: workspaceCloneClearOp(a),
			GitClone:     workspaceGitClone,
		},
		Backend: appworkspacecmd.BackendConfigDeps{
			BackendWorkspaceSwitchBindingNotice:        bcfg.backendWorkspaceSwitchBindingNotice,
			BackendWorkspaceSwitchBindingFailureNotice: bcfg.backendWorkspaceSwitchBindingFailureNotice,
			BackendWorkspaceSwitchInFlightNotice:       bcfg.backendWorkspaceSwitchInFlightNotice,
			BackendWorkspaceCommandUsage:               bcfg.backendWorkspaceCommandUsage,
			BackendWorkspacePermissionCommand:          bcfg.handleBackendWorkspacePermissionCommand,
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
				return newWorkspaceRenderService(a).RenderWorkspaceNewCard(sessionKey, requestID, payload)
			},
			RenderCloneCard: func(sessionKey, requestID string, payload appworkspacecmd.ClonePayload) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceCloneCard(sessionKey, requestID, payload)
			},
			RenderClonePreparingCard: func(requestID string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceClonePreparingCard(requestID, payload, parentDir, snapshot)
			},
			RenderCloneSuccessCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir)
			},
			RenderWorktreeCard: func(sessionKey, requestID string, payload appworkspacecmd.WorktreePayload) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceWorktreeCard(sessionKey, requestID, payload)
			},
			RenderWorktreePreparingCard: func(requestID string, payload appworkspacecmd.WorktreePayload, plan *appworkspacecmd.WorktreePlan, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceWorktreePreparingCard(requestID, payload, plan, snapshot)
			},
			RenderWorktreeSuccessCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceWorktreeSuccessCard(sessionKey, workspaceID, targetDir)
			},
			RenderWorktreeManualHintCard: func(sessionKey, workspaceID, targetDir, errText string) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceWorktreeManualHintCard(sessionKey, workspaceID, targetDir, errText)
			},
			RenderWorktreeCanceledCard: func(sessionKey string, payload appworkspacecmd.WorktreePayload, plan *appworkspacecmd.WorktreePlan, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceWorktreeCanceledCard(sessionKey, payload, plan, snapshot)
			},
			RenderSwitchExistingCard: func(sessionKey, workspaceID, targetDir, notice string) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, notice)
			},
			RenderCloneSwitchExistingCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
			},
			RenderCloneManualHintCard: func(sessionKey, workspaceID, targetDir, errText string) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceCloneManualHintCard(sessionKey, workspaceID, targetDir, errText)
			},
			RenderCloneCanceledCard: func(sessionKey string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceCloneCanceledCard(sessionKey, payload, parentDir, snapshot)
			},
			RenderMenuCard: func(sessionKey string) map[string]any {
				return newWorkspaceRenderService(a).RenderWorkspaceMenuCard(sessionKey)
			},
		},
	})
}

func workspaceCloneSetOp(a *App) func(string, *appworkspacecmd.CloneOperation) {
	return func(requestID string, op *appworkspacecmd.CloneOperation) {
		if a == nil {
			return
		}
		requestID = strings.TrimSpace(requestID)
		if requestID == "" || op == nil {
			return
		}
		trackers := a.Trackers()
		tracker := trackers.workspaceCloneOps
		if tracker == nil {
			tracker = newWorkspaceCloneTracker()
			trackers.workspaceCloneOps = tracker
		}
		tracker.Mu.Lock()
		defer tracker.Mu.Unlock()
		if tracker.Ops == nil {
			tracker.Ops = map[string]*appworkspacecmd.CloneOperation{}
		}
		if previous := tracker.Ops[requestID]; previous != nil && previous.Cancel != nil && previous != op {
			previous.Cancel()
		}
		tracker.Ops[requestID] = op
	}
}

func workspaceCloneGetOp(a *App) func(string) *appworkspacecmd.CloneOperation {
	return func(requestID string) *appworkspacecmd.CloneOperation {
		if a == nil {
			return nil
		}
		tracker := a.Trackers().workspaceCloneOps
		if tracker == nil {
			return nil
		}
		tracker.Mu.Lock()
		defer tracker.Mu.Unlock()
		return tracker.Ops[strings.TrimSpace(requestID)]
	}
}

func workspaceCloneClearOp(a *App) func(string) {
	return func(requestID string) {
		if a == nil {
			return
		}
		tracker := a.Trackers().workspaceCloneOps
		if tracker == nil {
			return
		}
		tracker.Mu.Lock()
		defer tracker.Mu.Unlock()
		delete(tracker.Ops, strings.TrimSpace(requestID))
	}
}
