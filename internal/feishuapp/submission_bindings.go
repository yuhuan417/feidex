package feishuapp

import (
	appturnstream "feidex/internal/adapter/feishu/turnstream"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/announcement"
	conversationapp "feidex/internal/application/conversation"
	appplan "feidex/internal/application/plan"
	appworkspace "feidex/internal/application/workspace"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/interaction"
	domainsubmission "feidex/internal/domain/submission"
	frontendruntime "feidex/internal/runtime"
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

type sqLiveThreadAdapter struct {
	tracker      *frontendruntime.LiveThreads
	session      func(string) *conversation.Session
	groupQuery   announcement.Query
	refreshGroup func(string)
}

func (a sqLiveThreadAdapter) MarkSessionThreadLive(sessionKey, threadID string) {
	if strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return
	}
	a.tracker.Mark(sessionKey, threadID)
	if sess := a.session(sessionKey); sess != nil {
		chatID := strings.TrimSpace(sess.ChatID)
		if chatID == "" {
			_, _, chatID, _, _ = identity.ParseSessionKey(sess.Key)
		}
		if sessionMatchesGroupChat(a.groupQuery, sess, chatID) {
			a.refreshGroup(chatID)
		}
	}
}
func (a sqLiveThreadAdapter) SessionHasLiveThread(sessionKey, threadID string) bool {
	if strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return false
	}
	return a.tracker.Has(sessionKey, threadID)
}
func (a sqLiveThreadAdapter) ClearSessionLiveThread(sessionKey string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	a.tracker.Clear(sessionKey)
}

func SubmissionLiveThreads(tracker *frontendruntime.LiveThreads, session func(string) *conversation.Session, groupQuery announcement.Query, refreshGroup func(string)) appsubmission.QueueLiveThreadProvider {
	return sqLiveThreadAdapter{tracker: tracker, session: session, groupQuery: groupQuery, refreshGroup: refreshGroup}
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

type sqAttachmentResolverFullAdapter struct {
	cfg          *config.Config
	contextFn    func() context.Context
	feishuClient FeishuClient
}

func (a sqAttachmentResolverFullAdapter) ResolveInboundAttachments(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]domainsubmission.SubmissionAttachment, error) {
	return resolveInboundAttachments(a.cfg, a.contextFn, a.feishuClient, msg, workspaceID, sessionKey)
}

type sqBackendRuntimeAdapter struct {
	deps         BackendRuntimeDeps
	backendOwner *frontendruntime.FrontendOwner
}

func (a sqBackendRuntimeAdapter) currentDeps() BackendRuntimeDeps {
	deps := a.deps
	if a.backendOwner != nil {
		deps.view.backend = a.backendOwner.Backend()
	}
	return deps
}

func (a sqBackendRuntimeAdapter) ReconcileCompletedTurnFromFinalOutput(sessionKey string, sess *conversation.Session) *conversation.Session {
	deps := a.currentDeps()
	if runtime := frontendruntime.BackendForKind(deps.view.configuredBackend()); runtime != nil {
		return runtime.ReconcileCompletedTurnFromFinalOutput(backendRuntimeContextForApp(deps), sessionKey, sess)
	}
	return sess
}
func (a sqBackendRuntimeAdapter) DropThreadLineageAfterStartFailure(err error) bool {
	deps := a.currentDeps()
	if runtime := frontendruntime.BackendForKind(deps.view.configuredBackend()); runtime != nil {
		return runtime.DropThreadLineageAfterStartFailure(backendRuntimeContextForApp(deps), err)
	}
	return false
}
func (a sqBackendRuntimeAdapter) DeferQueuedSubmissionsDuringRecovery() bool {
	deps := a.currentDeps()
	if runtime := frontendruntime.BackendForKind(deps.view.configuredBackend()); runtime != nil {
		return runtime.DeferQueuedSubmissionsDuringRecovery(backendRuntimeContextForApp(deps))
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

type submissionStartTracker interface {
	TryBegin(string) bool
	Finish(string) bool
}

type SubmissionPortInputs struct {
	Plan                      *appplan.Service
	TurnPresentation          *appturnstream.Service
	LiveThreads               appsubmission.QueueLiveThreadProvider
	Config                    *config.Config
	ConfigMu                  *sync.RWMutex
	FrontendID                string
	FrontendConfigIndex       int
	State                     *appstate.Store
	Context                   func() context.Context
	Feishu                    FeishuClient
	RuntimeOwner              *frontendruntime.FrontendOwner
	RuntimeDeps               BackendRuntimeDeps
	AsyncRunner               func(func())
	PendingQueue              appsubmission.QueuePendingQueueProvider
	SkillResolver             appsubmission.QueueSkillResolver
	Continuation              appsubmission.QueueReplyContinuationProvider
	TurnItems                 interface{ ClearTurn(string) }
	RuntimeMaintenance        appsubmission.QueueRuntimeMaintenanceProvider
	AutoRetry                 appsubmission.QueueAutoRetryProvider
	WorkspaceSelection        appworkspace.SelectionService
	ModelSettings             appsubmission.ModelSettings
	ConversationConfiguration conversationapp.Configuration
	Starts                    submissionStartTracker
	QueuedNotice              func(context.Context, *domainsubmission.Submission)
	ExpirePlan                func(context.Context, *interaction.PendingRequest)
	ReplyText                 func(context.Context, string, string, bool) error
	MarkQueued                func(*domainsubmission.Submission)
	MarkRunning               func(*domainsubmission.Submission)
	ClearProcessing           func(*domainsubmission.Submission)
	StartTurn                 func(context.Context, string, string, *domainsubmission.Submission, string, string, string, string, string, string, string) (string, error)
	StartReview               func(context.Context, string, *domainsubmission.Submission) (string, error)
}

func SubmissionPorts(inputs SubmissionPortInputs) appsubmission.Dependencies {
	cfg, configMu := inputs.Config, inputs.ConfigMu
	frontendID, frontendConfigIndex := inputs.FrontendID, inputs.FrontendConfigIndex
	stateStore, runtimeOwner := inputs.State, inputs.RuntimeOwner
	contextFn, feishuClient := inputs.Context, inputs.Feishu
	configView := frontendConfigView{cfg: cfg, mu: configMu, frontendID: frontendID, frontendConfigIndex: frontendConfigIndex}
	configuredBackend := ConfiguredBackendBuilder(cfg, configMu, runtimeOwner.Backend, frontendID, frontendConfigIndex)
	defaultWorkspaceID := func() string { return configView.defaultWorkspaceID() }
	runtime := runtimeView{owner: runtimeOwner}
	return appsubmission.Dependencies{
		PlanConfirmation:   inputs.Plan,
		PlanExpired:        inputs.ExpirePlan,
		Context:            contextFn,
		AppState:           stateStore,
		SkillResolver:      inputs.SkillResolver,
		AttachmentResolver: sqAttachmentResolverFullAdapter{cfg: cfg, contextFn: contextFn, feishuClient: feishuClient},
		LiveThread:         inputs.LiveThreads,
		PendingQueue:       inputs.PendingQueue,
		RuntimeState:       runtimeOwner.TurnBindings,
		Items:              inputs.TurnItems,
		RuntimeMaintenance: inputs.RuntimeMaintenance,
		ReplyContinuation:  inputs.Continuation,
		TurnStream:         inputs.TurnPresentation,
		AutoRetry:          inputs.AutoRetry,

		BackendRuntime: sqBackendRuntimeAdapter{deps: inputs.RuntimeDeps, backendOwner: runtimeOwner},
		DefaultWorkspaceID: func() string {
			return defaultWorkspaceID()
		},
		Workspace: func(id string) *config.Workspace {
			return config.FindWorkspace(cfg, id)
		},
		ReplyInThreadEnabled: func(chatType string) bool {
			return configView.replyInThreadEnabled()
		},
		ReplyInThreadForSubmission: func(sub *domainsubmission.Submission) bool {
			return replyInThreadForSubmission(sub)
		},
		ConfiguredInflightMode: func() appsubmission.QueueInflightMode {
			return inflightModeToInt(configuredSessionInflightMode(configuredBackend))
		},
		InflightAllowsAdditional: func(mode appsubmission.QueueInflightMode) bool {
			return sessionInflightAllowsAdditional(intToInflightMode(mode))
		},
		ResolveWorkspaceID: func(msg *feishu.InboundMessage, sess *conversation.Session, bindOnlyCurrentRoot bool) string {
			return resolveSubmissionWorkspaceID(stateStore, inputs.WorkspaceSelection, defaultWorkspaceID, msg, sess, bindOnlyCurrentRoot)
		},
		ReplyText: func(ctx context.Context, messageID, text string, inThread bool) error {
			return inputs.ReplyText(ctx, messageID, text, inThread)
		},
		SendQueuedNotice: inputs.QueuedNotice,
		RunSessionAsync: func(sessionKey string, fn func()) {
			if fn == nil {
				return
			}
			runtimeOwner.Lifecycle.Run(func() { runtimeOwner.SessionActors.Run("session:"+strings.TrimSpace(sessionKey), fn) }, inputs.AsyncRunner)
		},
		TryBeginStart: func(sessionKey string) bool {
			return inputs.Starts.TryBegin(sessionKey)
		},
		FinishStart: func(sessionKey string) bool {
			return inputs.Starts.Finish(sessionKey)
		},
		LogSessionState: func(event, sessionKey string, sess *conversation.Session) {
			logSessionState(event, sessionKey, sess)
		},
		MarkSubmissionQueuedReactions:      inputs.MarkQueued,
		MarkSubmissionRunningReactions:     inputs.MarkRunning,
		ClearSubmissionProcessingReactions: inputs.ClearProcessing,
		IsReviewSubmission: func(sub *domainsubmission.Submission) bool {
			return appreviewcmd.IsReviewSubmission(sub)
		},
		StartSubmissionTurn: func(ctx context.Context, sessionKey, threadID string, sub *domainsubmission.Submission, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort, multiAgentMode string) (string, error) {
			return inputs.StartTurn(ctx, sessionKey, threadID, sub, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort, multiAgentMode)
		},
		StartSubmissionReview: func(ctx context.Context, threadID string, sub *domainsubmission.Submission) (string, error) {
			return inputs.StartReview(ctx, threadID, sub)
		},
		StartConversation: func(ctx context.Context, ws *config.Workspace, sess *conversation.Session, sub *domainsubmission.Submission, model string) (appsubmission.ConversationStarted, error) {
			client, err := runtime.requireCodexClient()
			if err != nil {
				return appsubmission.ConversationStarted{}, err
			}
			return codexadapter.StartConversation(ctx, client, inputs.ConversationConfiguration.ThreadStart(conversationapp.Request{Workspace: ws, Session: sess, Model: model}), sub.ModelConfig)
		},
		DeleteTurnArtifacts: stateStore.DeleteTurnArtifacts,
		ClaudePrompt:        claudeadapter.BuildPrompt,
		Backend:             configuredBackend,
		ClaudeClient: func() appsubmission.QueueClaudeClient {
			if runtime.currentClaudeCore() == nil {
				return nil
			}
			return claudeClientAdapter{claude: runtime.currentClaudeCore()}
		},
		AgentBinding: func(chatType, chatID string) *state.AgentBinding {
			return agentBindingForChat(stateStore, chatType, chatID)
		},
		AgentBindingByID: func(id string) *state.AgentBinding {
			return stateStore.AgentBinding(id)
		},
		BotProfile: func() *state.BotProfile {
			return stateStore.BotProfile()
		},
		ModelSettings: inputs.ModelSettings,
	}
}

func findSubmissionByTurn(lookup appsubmission.SubmissionLookupService, threadID, turnID string) (string, *domainsubmission.Submission) {
	return lookup.FindSubmissionByTurn(threadID, turnID)
}
