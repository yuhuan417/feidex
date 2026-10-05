package feishuapp

import (
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/announcement"
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
type WorkspaceManagementInputs struct {
	Dependencies        appworkspacecmd.Dependencies
	State               *appstate.Store
	RuntimeOwner        *appruntime.FrontendOwner
	AsyncRunner         func(func())
	Conversations       *conversationapp.Service
	BindingScope        BindingScope
	AnnouncementQuery   announcement.Query
	CompleteMenuCommand appworkspacecmd.CompleteMenuCommandFn
	FrontendID          string
	Effects             appruntime.EffectRunner
	ReplyInThread       bool
	Presentation        *workspacecards.Presentation
}

func BuildWorkspaceManagementService(inputs WorkspaceManagementInputs) *appworkspacecmd.ManagementService {
	dependencies := inputs.Dependencies
	st := inputs.State
	replyRunner := inputs.Effects
	frontendID := inputs.FrontendID
	bindingScope := inputs.BindingScope.scope
	threadMarker := liveThreadMarker{
		tracker: inputs.RuntimeOwner.LiveThreads, state: st,
		announcement: inputs.AnnouncementQuery, refreshes: inputs.RuntimeOwner.Announcements,
	}
	return appworkspacecmd.NewManagementService(appworkspacecmd.ManagementDeps{
		Dependencies: dependencies,
		State:        workspaceStateDeps(st),
		SessionContext: appworkspacecmd.SessionContextDeps{
			SessionHasInFlight:     conversation.HasInFlightSubmission,
			ClearSessionLiveThread: threadMarker.tracker.Clear,
		},
		Threads: appworkspacecmd.ThreadDeps{
			EnsureWorkspaceThreadBinding: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return inputs.Conversations.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
			},
			MarkSessionThreadLive:  threadMarker.MarkSessionThreadLive,
			ClearSessionLiveThread: threadMarker.tracker.Clear,
			StartWorkspaceThread: func(sessionKey string, sess *conversation.Session, ws *config.Workspace) (*appworkspacecmd.ThreadBinding, error) {
				return inputs.Conversations.StartWorkspaceThread(sessionKey, sess, ws)
			},
		},
		Clone: appworkspacecmd.CloneDeps{
			SetCloneOp:   workspaceCloneSetOp(inputs.RuntimeOwner),
			GetCloneOp:   workspaceCloneGetOp(inputs.RuntimeOwner),
			ClearCloneOp: workspaceCloneClearOp(inputs.RuntimeOwner),
			GitClone:     workspaceGitClone,
		},
		Backend: workspaceBackendConfigDeps(dependencies.BackendDriver),
		Actions: appworkspacecmd.ActionDeps{
			CompleteMenuCommand: inputs.CompleteMenuCommand,
			ReplyCommandActionResponse: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
				return replyCommandActionResponseWith(replyRunner, frontendID, inputs.ReplyInThread, msg, resp)
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
			RunAsync: func(fn func()) { runAsync(&inputs.RuntimeOwner.Lifecycle, inputs.AsyncRunner, fn) },
		},
		Render: appworkspacecmd.ManagementRenderDeps{
			RenderNewCard: func(sessionKey, requestID string, payload appworkspacecmd.NewPayload) map[string]any {
				return inputs.Presentation.RenderWorkspaceNewCard(sessionKey, requestID, payload)
			},
			RenderCloneCard: func(sessionKey, requestID string, payload appworkspacecmd.ClonePayload) map[string]any {
				return inputs.Presentation.RenderWorkspaceCloneCard(sessionKey, requestID, payload)
			},
			RenderClonePreparingCard: func(requestID string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return inputs.Presentation.RenderWorkspaceClonePreparingCard(requestID, payload, parentDir, snapshot)
			},
			RenderCloneSuccessCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return inputs.Presentation.RenderWorkspaceCloneSuccessCard(sessionKey, workspaceID, targetDir)
			},
			RenderWorktreeCard: func(sessionKey, requestID string, payload appworkspacecmd.WorktreePayload) map[string]any {
				return inputs.Presentation.RenderWorkspaceWorktreeCard(sessionKey, requestID, payload)
			},
			RenderWorktreePreparingCard: func(requestID string, payload appworkspacecmd.WorktreePayload, plan *appworkspacecmd.WorktreePlan, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return inputs.Presentation.RenderWorkspaceWorktreePreparingCard(requestID, payload, plan, snapshot)
			},
			RenderWorktreeSuccessCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return inputs.Presentation.RenderWorkspaceWorktreeSuccessCard(sessionKey, workspaceID, targetDir)
			},
			RenderWorktreeManualHintCard: func(sessionKey, workspaceID, targetDir, errText string) map[string]any {
				return inputs.Presentation.RenderWorkspaceWorktreeManualHintCard(sessionKey, workspaceID, targetDir, errText)
			},
			RenderWorktreeCanceledCard: func(sessionKey string, payload appworkspacecmd.WorktreePayload, plan *appworkspacecmd.WorktreePlan, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return inputs.Presentation.RenderWorkspaceWorktreeCanceledCard(sessionKey, payload, plan, snapshot)
			},
			RenderSwitchExistingCard: func(sessionKey, workspaceID, targetDir, notice string) map[string]any {
				return inputs.Presentation.RenderWorkspaceSwitchExistingCard(sessionKey, workspaceID, targetDir, notice)
			},
			RenderCloneSwitchExistingCard: func(sessionKey, workspaceID, targetDir string) map[string]any {
				return inputs.Presentation.RenderWorkspaceCloneSwitchExistingCard(sessionKey, workspaceID, targetDir)
			},
			RenderCloneManualHintCard: func(sessionKey, workspaceID, targetDir, errText string) map[string]any {
				return inputs.Presentation.RenderWorkspaceCloneManualHintCard(sessionKey, workspaceID, targetDir, errText)
			},
			RenderCloneCanceledCard: func(sessionKey string, payload appworkspacecmd.ClonePayload, parentDir string, snapshot appworkspacecmd.CloneProgressSnapshot) map[string]any {
				return inputs.Presentation.RenderWorkspaceCloneCanceledCard(sessionKey, payload, parentDir, snapshot)
			},
			RenderMenuCard: func(sessionKey string) map[string]any {
				return inputs.Presentation.RenderWorkspaceMenuCard(sessionKey)
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
