package feishuapp

import (
	"feidex/internal/adapter/feishu/planmode"
	appturnstream "feidex/internal/adapter/feishu/turnstream"
	appstate "feidex/internal/adapter/storage/json/scoped"
	conversationapp "feidex/internal/application/conversation"
	appplan "feidex/internal/application/plan"
	"feidex/internal/domain/interaction"
	domainsubmission "feidex/internal/domain/submission"
	runtimemaintenance "feidex/internal/runtime/maintenance"
	"sync"

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
	return resolveInboundAttachments(a.app.cfg, a.app.Context, a.app.feishu, msg, workspaceID, sessionKey)
}

type sqBackendRuntimeFullAdapter struct{ app *App }

func (a sqBackendRuntimeFullAdapter) ReconcileCompletedTurnFromFinalOutput(sessionKey string, sess *conversation.Session) *conversation.Session {
	if runtime := backendRuntime(a.app); runtime != nil {
		return runtime.ReconcileCompletedTurnFromFinalOutput(backendRuntimeContextForApp(a.app.BackendRuntimeDeps()), sessionKey, sess)
	}
	return sess
}
func (a sqBackendRuntimeFullAdapter) DropThreadLineageAfterStartFailure(err error) bool {
	if runtime := backendRuntime(a.app); runtime != nil {
		return runtime.DropThreadLineageAfterStartFailure(backendRuntimeContextForApp(a.app.BackendRuntimeDeps()), err)
	}
	return false
}
func (a sqBackendRuntimeFullAdapter) DeferQueuedSubmissionsDuringRecovery() bool {
	if runtime := backendRuntime(a.app); runtime != nil {
		return runtime.DeferQueuedSubmissionsDuringRecovery(backendRuntimeContextForApp(a.app.BackendRuntimeDeps()))
	}
	return false
}

// ---------------------------------------------------------------------------
// PendingQueueApp adapter — implements submission.PendingQueueApp for *App
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Convenience constructors
// ---------------------------------------------------------------------------

// PendingQueuePorts takes the values it needs instead of the frontend
// aggregate, so composition can supply them from what it already holds.
func PendingQueuePorts(
	contextFn func() context.Context,
	store *appstate.Store,
	cleanup runtimemaintenance.SubmissionCleanup,
	cfg *config.Config,
	mu *sync.RWMutex,
	feishuClient FeishuClient,
) appsubmission.PendingDependencies {
	config := frontendConfigView{cfg: cfg, mu: mu}
	return appsubmission.PendingDependencies{
		Context:            contextFn,
		State:              store,
		Maintenance:        cleanup,
		DefaultWorkspaceID: func() string { return config.defaultWorkspaceID() },
		AddReaction: func(ctx context.Context, messageID, emoji string) error {
			if feishuClient == nil {
				return nil
			}
			return feishuClient.AddReaction(ctx, messageID, emoji)
		},
		RemoveReaction: func(ctx context.Context, messageID, emoji string) error {
			if feishuClient == nil {
				return nil
			}
			return feishuClient.RemoveReaction(ctx, messageID, emoji)
		},
		LogSessionState: logSessionState,
	}
}

func (a claudeClientAdapter) CanRetryFreshSession(sessionKey string) bool {
	if runtime, ok := a.claude.(interface{ CanRetryFreshSession(string) bool }); ok {
		return runtime.CanRetryFreshSession(sessionKey)
	}
	return true
}

func SubmissionPorts(a *App, plan *appplan.Service, turnPresentation *appturnstream.Service) appsubmission.Dependencies {
	// Read the sibling services once, at construction time, so the dependency
	// is visible instead of hidden in the closures below.
	pendingQueue := a.bindings.PendingQueue
	turnStarter := a.bindings.TurnStarter
	review := a.bindings.Review
	conversationConfiguration := a.bindings.ConversationConfiguration
	return appsubmission.Dependencies{
		PlanConfirmation: plan,
		PlanExpired: func(ctx context.Context, pending *interaction.PendingRequest) {
			if pending.FeishuMsgID != "" {
				_ = patchCardEffect(ctx, a, pending.FeishuMsgID, planmode.ExitExpiredCard(newPlanModeAppAdapter(a), pending.SessionKey, "", "当前已有新的提交，旧的计划确认已失效。"))
			}
		},
		Context:            a.Context,
		AppState:           a.State(),
		SkillResolver:      a.bindings.Skills,
		AttachmentResolver: sqAttachmentResolverFullAdapter{app: a},
		LiveThread:         sqLiveThreadAdapter{app: a},
		PendingQueue:       a.bindings.Continuation,
		RuntimeState:       a.runtimeOwner.TurnBindings,
		Items:              a.bindings.TurnItems,
		RuntimeMaintenance: a.bindings.SubmissionCleanup,
		ReplyContinuation:  a.bindings.Continuation,
		TurnStream:         turnPresentation,
		AutoRetry:          a.bindings.AutoRetry,

		BackendRuntime: sqBackendRuntimeFullAdapter{app: a},
		DefaultWorkspaceID: func() string {
			return a.configView().defaultWorkspaceID()
		},
		Workspace: func(id string) *config.Workspace {
			return config.FindWorkspace(a.cfg, id)
		},
		ReplyInThreadEnabled: func(chatType string) bool {
			return a.configView().replyInThreadEnabled()
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
			pendingQueue.MarkSubmissionQueuedReactions(sub)
		},
		MarkSubmissionRunningReactions: func(sub *domainsubmission.Submission) {
			pendingQueue.MarkSubmissionRunningReactions(sub)
		},
		ClearSubmissionProcessingReactions: func(sub *domainsubmission.Submission) {
			pendingQueue.ClearSubmissionProcessingReactions(sub)
		},
		IsReviewSubmission: func(sub *domainsubmission.Submission) bool {
			return appreviewcmd.IsReviewSubmission(sub)
		},
		StartSubmissionTurn: func(ctx context.Context, sessionKey, threadID string, sub *domainsubmission.Submission, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort, multiAgentMode string) (string, error) {
			return turnStarter.Start(ctx, sessionKey, threadID, sub, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort, multiAgentMode)
		},
		StartSubmissionReview: func(ctx context.Context, threadID string, sub *domainsubmission.Submission) (string, error) {
			return review.StartSubmission(ctx, threadID, sub)
		},
		StartConversation: func(ctx context.Context, ws *config.Workspace, sess *conversation.Session, sub *domainsubmission.Submission, model string) (appsubmission.ConversationStarted, error) {
			client, err := a.runtimeView().requireCodexClient()
			if err != nil {
				return appsubmission.ConversationStarted{}, err
			}
			return codexadapter.StartConversation(ctx, client, conversationConfiguration.ThreadStart(conversationapp.Request{Workspace: ws, Session: sess, Model: model}), sub.ModelConfig)
		},
		DeleteTurnArtifacts: a.State().DeleteTurnArtifacts,
		ClaudePrompt:        claudeadapter.BuildPrompt,
		Backend:             func() string { return a.configView().configuredBackend() },
		ClaudeClient: func() appsubmission.QueueClaudeClient {
			if a.runtimeView().currentClaudeCore() == nil {
				return nil
			}
			return claudeClientAdapter{claude: a.runtimeView().currentClaudeCore()}
		},
		AgentBinding: func(chatType, chatID string) *state.AgentBinding {
			return agentBindingForChat(a.State(), chatType, chatID)
		},
		AgentBindingByID: func(id string) *state.AgentBinding {
			return a.State().AgentBinding(id)
		},
		BotProfile: func() *state.BotProfile {
			return a.State().BotProfile()
		},
		ModelSettings: a.bindings.ModelSnapshots,
	}
}

func findSubmissionByTurn(lookup appsubmission.SubmissionLookupService, threadID, turnID string) (string, *domainsubmission.Submission) {
	return lookup.FindSubmissionByTurn(threadID, turnID)
}
