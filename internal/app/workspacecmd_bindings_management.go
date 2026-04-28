package app

import (
	"strings"

	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newWorkspaceManagementServiceInner(a *App) *appworkspacecmd.ManagementService {
	st := a.State()
	bcfg := newBackendConfigurationService(a)
	return appworkspacecmd.NewManagementService(appworkspacecmd.ManagementDeps{
		App:   a,
		State: workspaceStateDeps(st),
		SessionContext: appworkspacecmd.SessionContextDeps{
			SessionHasInFlight:     sessionHasInFlightSubmission,
			SwitchSessionWorkspace: switchSessionWorkspace,
			ClearSessionThreadCtx:  clearSessionThreadContext,
			SetSessionThreadCtx:    setSessionThreadContext,
			SessionResetActiveOps:  sessionResetActiveOperations,
			ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },
		},
		Threads: appworkspacecmd.ThreadDeps{
			EnsureWorkspaceThreadBinding: func(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return newWorkspaceThreadServiceInner(a).EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
			},
			MarkSessionThreadLive:  func(sessionKey, threadID string) { markSessionThreadLive(a, sessionKey, threadID) },
			ClearSessionLiveThread: func(sessionKey string) { clearSessionLiveThread(a, sessionKey) },
			StartWorkspaceThread: func(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return newWorkspaceThreadServiceInner(a).StartWorkspaceThread(sessionKey, sess, ws)
			},
		},
		Clone: appworkspacecmd.CloneDeps{
			SetCloneOp:   workspaceCloneSetOp(a),
			GetCloneOp:   workspaceCloneGetOp(a),
			ClearCloneOp: workspaceCloneClearOp(a),
			GitClone:     workspaceGitClone,
		},
		Codex: appworkspacecmd.CodexDeps{
			RequireCodexClient: func() (appworkspacecmd.CodexClient, error) { return requireCodexClient(a) },
			BuildThreadStartParams: func(ws *config.Workspace, sess *state.Session, effectiveModel string) codexrpc.ThreadStartParams {
				return buildThreadStartParams(a, ws, sess, effectiveModel)
			},
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
				return indirectCompleteMenuCommand(a, action, sessionKey, rawCommand, parentAction)
			},
			ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
				return indirectReplyCommandActionResponse(a, msg, resp)
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
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceNewCard(sessionKey, requestID, payload)
			},
			RenderCloneCard: func(sessionKey, requestID string, payload appworkspacecmd.ClonePayload) map[string]any {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneCard(sessionKey, requestID, payload)
			},
			RenderClonePreparingCard: func(requestID string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceClonePreparingCard(requestID, payload, parentDir, snapshot)
			},
			RenderCloneSuccessCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir)
			},
			RenderSwitchExistingCard: func(sessionKey, workspaceID, targetDir, notice string) map[string]any {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, notice)
			},
			RenderCloneSwitchExistingCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
			},
			RenderCloneManualHintCard: func(sessionKey, workspaceID, targetDir, errText string) map[string]any {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneManualHintCard(sessionKey, workspaceID, targetDir, errText)
			},
			RenderCloneCanceledCard: func(sessionKey string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceCloneCanceledCard(sessionKey, payload, parentDir, snapshot)
			},
			RenderMenuCard: func(sessionKey string) map[string]any {
				return newWorkspaceRenderServiceInner(a).RenderWorkspaceMenuCard(sessionKey)
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
		tracker := a.trackers.workspaceCloneOps
		if tracker == nil {
			tracker = newWorkspaceCloneTracker()
			a.trackers.workspaceCloneOps = tracker
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
		tracker := a.trackers.workspaceCloneOps
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
		tracker := a.trackers.workspaceCloneOps
		if tracker == nil {
			return
		}
		tracker.Mu.Lock()
		defer tracker.Mu.Unlock()
		delete(tracker.Ops, strings.TrimSpace(requestID))
	}
}
