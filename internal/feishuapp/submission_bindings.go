package feishuapp

import (
	domainsubmission "feidex/internal/domain/submission"

	"context"
	"feidex/internal/domain/conversation"
	"strings"

	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"

	claudeadapter "feidex/internal/adapter/backend/claude"
	codexadapter "feidex/internal/adapter/backend/codex"
	appsubmission "feidex/internal/application/submission"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type sqLiveThreadAdapter struct{ app *App }

func (a sqLiveThreadAdapter) MarkSessionThreadLive(sessionKey, threadID string) {
	markSessionThreadLive(a.app, sessionKey, threadID)
}
func (a sqLiveThreadAdapter) SessionHasLiveThread(sessionKey, threadID string) bool {
	return sessionHasLiveThread(a.app, sessionKey, threadID)
}
func (a sqLiveThreadAdapter) ClearSessionLiveThread(sessionKey string) {
	clearSessionLiveThread(a.app, sessionKey)
}

// ---------------------------------------------------------------------------
// App adapter — implements submission.App for *App
// ---------------------------------------------------------------------------

func inflightModeToInt(m sessionInflightMode) appsubmission.QueueInflightMode {
	switch m {
	case "serialized":
		return 1
	case "parallel":
		return 2
	default:
		return 0
	}
}

func intToInflightMode(m appsubmission.QueueInflightMode) sessionInflightMode {
	switch m {
	case 1:
		return "serialized"
	case 2:
		return "parallel"
	default:
		return "single"
	}
}

// ---------------------------------------------------------------------------
// Claude client adapter
// ---------------------------------------------------------------------------

type claudeClientAdapter struct{ claude ClaudeCore }

func (a claudeClientAdapter) EnsureSession(ctx context.Context, sessionKey string, ws *config.Workspace, resumeThreadID, model string) (string, error) {
	return a.claude.EnsureSession(ctx, sessionKey, ws, resumeThreadID, model)
}
func (a claudeClientAdapter) StartTurn(ctx context.Context, sessionKey, threadID, turnID, prompt string) error {
	return a.claude.StartTurn(ctx, sessionKey, threadID, turnID, prompt)
}
func (a claudeClientAdapter) StartSteerTurn(ctx context.Context, sessionKey, threadID, turnID, prompt, steerSubmissionID string) error {
	return a.claude.StartSteerTurn(ctx, sessionKey, threadID, turnID, prompt, steerSubmissionID)
}

// ---------------------------------------------------------------------------
// Full adapter types for providers that need app access
// ---------------------------------------------------------------------------

type sqAttachmentResolverFullAdapter struct{ app *App }

func (a sqAttachmentResolverFullAdapter) ResolveInboundAttachments(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]domainsubmission.SubmissionAttachment, error) {
	return resolveInboundAttachments(a.app, msg, workspaceID, sessionKey)
}

type sqPendingQueueFullAdapter struct{ app *App }

func (a sqPendingQueueFullAdapter) PendingInputSessionKey(msg *feishu.InboundMessage) string {
	return newReplyContinuationService(a.app).PendingInputSessionKey(msg)
}
func (a sqPendingQueueFullAdapter) CollectPendingStagedImages(sessionKey, bucketSessionKey string) []conversation.SessionStagedImage {
	return newReplyContinuationService(a.app).CollectPendingStagedImages(sessionKey, bucketSessionKey)
}
func (a sqPendingQueueFullAdapter) ClearPendingStagedImages(sessionKey, bucketSessionKey string) error {
	return newReplyContinuationService(a.app).ClearPendingStagedImages(sessionKey, bucketSessionKey)
}

type sqBackendRuntimeFullAdapter struct{ app *App }

func (a sqBackendRuntimeFullAdapter) ReconcileCompletedTurnFromFinalOutput(sessionKey string, sess *conversation.Session) *conversation.Session {
	if runtime := backendRuntime(a.app); runtime != nil {
		return runtime.reconcileCompletedTurnFromFinalOutput(backendRuntimeContextForApp(a.app), sessionKey, sess)
	}
	return sess
}
func (a sqBackendRuntimeFullAdapter) DropThreadLineageAfterStartFailure(err error) bool {
	if runtime := backendRuntime(a.app); runtime != nil {
		return runtime.dropThreadLineageAfterStartFailure(backendRuntimeContextForApp(a.app), err)
	}
	return false
}
func (a sqBackendRuntimeFullAdapter) DeferQueuedSubmissionsDuringRecovery() bool {
	if runtime := backendRuntime(a.app); runtime != nil {
		return runtime.deferQueuedSubmissionsDuringRecovery(backendRuntimeContextForApp(a.app))
	}
	return false
}

// ---------------------------------------------------------------------------
// PendingQueueApp adapter — implements submission.PendingQueueApp for *App
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Convenience constructors
// ---------------------------------------------------------------------------

func newPendingQueueServiceFromApp(a *App) appsubmission.PendingQueueService {
	return appsubmission.NewPendingQueueService(appsubmission.PendingDependencies{
		Context:            a.Context,
		State:              a.State(),
		Maintenance:        newSubmissionCleanup(a),
		DefaultWorkspaceID: func() string { return defaultWorkspaceID(a) },
		AddReaction: func(ctx context.Context, messageID, emoji string) error {
			if a.feishu == nil {
				return nil
			}
			return a.feishu.AddReaction(ctx, messageID, emoji)
		},
		RemoveReaction: func(ctx context.Context, messageID, emoji string) error {
			if a.feishu == nil {
				return nil
			}
			return a.feishu.RemoveReaction(ctx, messageID, emoji)
		},
		LogSessionState: logSessionState,
	})
}

func (a claudeClientAdapter) CanRetryFreshSession(sessionKey string) bool {
	if runtime, ok := a.claude.(interface{ CanRetryFreshSession(string) bool }); ok {
		return runtime.CanRetryFreshSession(sessionKey)
	}
	return true
}

func newSubmissionQueueServiceFromApp(a *App) appsubmission.SubmissionQueueService {
	return appsubmission.NewSubmissionQueueService(appsubmission.Dependencies{
		Context:            a.Context,
		AppState:           a.State(),
		SkillResolver:      newSkillUseCase(a),
		AttachmentResolver: sqAttachmentResolverFullAdapter{app: a},
		LiveThread:         sqLiveThreadAdapter{app: a},
		PendingQueue:       sqPendingQueueFullAdapter{app: a},
		RuntimeState:       newRuntimeStateService(a),
		RuntimeMaintenance: newSubmissionCleanup(a),
		ReplyContinuation:  newReplyContinuationService(a),
		TurnStream:         newTurnStreamService(a),
		AutoRetry:          newAutoRetryService(a),

		BackendRuntime: sqBackendRuntimeFullAdapter{app: a},
		DefaultWorkspaceID: func() string {
			return defaultWorkspaceID(a)
		},
		Workspace: func(id string) *config.Workspace {
			return config.FindWorkspace(a.cfg, id)
		},
		ReplyInThreadEnabled: func(chatType string) bool {
			return replyInThreadEnabled(a, chatType)
		},
		ReplyInThreadForSubmission: func(sub *domainsubmission.Submission) bool {
			return replyInThreadForSubmission(a, sub)
		},
		ConfiguredInflightMode: func() appsubmission.QueueInflightMode {
			return inflightModeToInt(configuredSessionInflightMode(a))
		},
		InflightAllowsAdditional: func(mode appsubmission.QueueInflightMode) bool {
			return sessionInflightAllowsAdditional(intToInflightMode(mode))
		},
		ResolveWorkspaceID: func(msg *feishu.InboundMessage, sess *conversation.Session, bindOnlyCurrentRoot bool) string {
			return resolveSubmissionWorkspaceID(a, msg, sess, bindOnlyCurrentRoot)
		},
		ReplyText: func(ctx context.Context, messageID, text string, inThread bool) error {
			return replyTextByAnchorEffect(ctx, a, messageID, text, inThread)
		},
		SendQueuedNotice: func(ctx context.Context, sub *domainsubmission.Submission) {
			sendSubmissionQueuedNotice(a, ctx, sub)
		},
		RunAsync: func(fn func()) {
			runAsync(a, fn)
		},
		RunSessionAsync: func(sessionKey string, fn func()) {
			if fn == nil {
				return
			}
			runAsync(a, func() { a.sessionActorRuntime().Run("session:"+strings.TrimSpace(sessionKey), fn) })
		},
		TryBeginStart: func(sessionKey string) bool {
			return submissionStartTracker(a).TryBegin(sessionKey)
		},
		FinishStart: func(sessionKey string) bool {
			return submissionStartTracker(a).Finish(sessionKey)
		},
		LogSessionState: func(event, sessionKey string, sess *conversation.Session) {
			logSessionState(event, sessionKey, sess)
		},
		MarkSubmissionQueuedReactions: func(sub *domainsubmission.Submission) {
			newPendingQueueService(a).markSubmissionQueuedReactions(sub)
		},
		MarkSubmissionRunningReactions: func(sub *domainsubmission.Submission) {
			newPendingQueueService(a).markSubmissionRunningReactions(sub)
		},
		ClearSubmissionProcessingReactions: func(sub *domainsubmission.Submission) {
			newPendingQueueService(a).clearSubmissionProcessingReactions(sub)
		},
		IsReviewSubmission: func(sub *domainsubmission.Submission) bool {
			return appreviewcmd.IsReviewSubmission(sub)
		},
		StartSubmissionTurn: func(ctx context.Context, sessionKey, threadID string, sub *domainsubmission.Submission, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort, multiAgentMode string) (string, error) {
			return startSubmissionTurn(a, ctx, sessionKey, threadID, sub, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort, multiAgentMode)
		},
		StartSubmissionReview: func(ctx context.Context, threadID string, sub *domainsubmission.Submission) (string, error) {
			return appreviewcmd.StartSubmissionReview(newReviewAppAdapter(a), ctx, threadID, sub)
		},
		StartConversation: func(ctx context.Context, ws *config.Workspace, sess *conversation.Session, sub *domainsubmission.Submission, model string) (appsubmission.ConversationStarted, error) {
			client, err := requireCodexClient(a)
			if err != nil {
				return appsubmission.ConversationStarted{}, err
			}
			return codexadapter.StartConversation(ctx, client, buildThreadStartParams(a, ws, sess, model), sub.ModelConfig)
		},
		DeleteTurnArtifacts: func(turnID string) {
			a.State().DeletePendingRequests(func(req *state.PendingRequest) bool {
				return req != nil && strings.TrimSpace(req.TurnID) == strings.TrimSpace(turnID)
			})
			a.State().DeleteMessageLinks(func(link *state.MessageLink) bool {
				return link != nil && strings.TrimSpace(link.TurnID) == strings.TrimSpace(turnID)
			})
		},
		ClaudePrompt: claudeadapter.BuildPrompt,
		Backend:      func() string { return configuredBackend(a) },
		ClaudeClient: func() appsubmission.QueueClaudeClient {
			if currentClaudeCore(a) == nil {
				return nil
			}
			return claudeClientAdapter{claude: currentClaudeCore(a)}
		},
		AgentBinding: func(chatType, chatID string) *state.AgentBinding {
			return agentBindingForChat(a, chatType, chatID)
		},
		AgentBindingByID: func(id string) *state.AgentBinding {
			return a.State().AgentBinding(id)
		},
		BotProfile: func() *state.BotProfile {
			return a.State().BotProfile()
		},
		ModelSettings: newModelSnapshotService(a),
	})
}

func findSubmissionByTurn(a *App, threadID, turnID string) (string, *domainsubmission.Submission) {
	if a == nil {
		return "", nil
	}
	return (appsubmission.SubmissionLookupService{State: a.State(), Runtime: newRuntimeStateService(a)}).FindSubmissionByTurn(threadID, turnID)
}
