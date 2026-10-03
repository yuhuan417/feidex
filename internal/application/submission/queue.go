// Package submission provides services for submission queueing, pending
// queue management, and related pure helpers. These services have no
// dependency on host; they communicate with the host through narrow
// provider interfaces injected at construction time.
package submission

import (
	"context"
	"errors"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	domaininteraction "feidex/internal/domain/interaction"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/domain/workspace"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/application"
	skillapp "feidex/internal/application/skill"
	domainmodelconfig "feidex/internal/domain/modelconfig"
	"feidex/internal/domain/routing"
)

// ---------------------------------------------------------------------------
// Explicit queue dependencies
// ---------------------------------------------------------------------------

// Dependencies owns the explicit inputs, repositories and external effects needed by queue orchestration.
// Composition supplies each dependency once; the service never calls a host to locate another service.
type Dependencies struct {
	Context                    func() context.Context
	Backend                    func() string
	StartConversation          func(context.Context, *workspace.Workspace, *conversation.Session, *domainsubmission.Submission, string) (ConversationStarted, error)
	DeleteTurnArtifacts        func(string)
	AppState                   QueueStateProvider
	SkillResolver              QueueSkillResolver
	AttachmentResolver         QueueAttachmentResolver
	LiveThread                 QueueLiveThreadProvider
	PendingQueue               QueuePendingQueueProvider
	RuntimeState               QueueRuntimeStateProvider
	Items                      interface{ ClearTurn(string) }
	RuntimeMaintenance         QueueRuntimeMaintenanceProvider
	ReplyContinuation          QueueReplyContinuationProvider
	TurnStream                 QueueTurnStreamProvider
	AutoRetry                  QueueAutoRetryProvider
	BackendRuntime             QueueBackendRuntimeProvider
	DefaultWorkspaceID         func() string
	Workspace                  func(id string) *workspace.Workspace
	ReplyInThreadEnabled       func(chatType string) bool
	ReplyInThreadForSubmission func(sub *domainsubmission.Submission) bool
	ConfiguredInflightMode     func() QueueInflightMode
	InflightAllowsAdditional   func(mode QueueInflightMode) bool
	ResolveWorkspaceID         func(msg *application.InboundMessage, sess *conversation.Session, bindOnlyCurrentRoot bool) string
	ReplyText                  func(ctx context.Context, messageID, text string, inThread bool) error
	SendQueuedNotice           func(ctx context.Context, sub *domainsubmission.Submission)
	// RunSessionAsync serializes asynchronous follow-up work with the session
	// actor.
	RunSessionAsync                    func(sessionKey string, fn func())
	TryBeginStart                      func(sessionKey string) bool
	FinishStart                        func(sessionKey string) bool
	LogSessionState                    func(event, sessionKey string, sess *conversation.Session)
	MarkSubmissionQueuedReactions      func(sub *domainsubmission.Submission)
	MarkSubmissionRunningReactions     func(sub *domainsubmission.Submission)
	ClearSubmissionProcessingReactions func(sub *domainsubmission.Submission)
	IsReviewSubmission                 func(sub *domainsubmission.Submission) bool
	StartSubmissionTurn                func(ctx context.Context, sessionKey, threadID string, sub *domainsubmission.Submission, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort, multiAgentMode string) (string, error)
	StartSubmissionReview              func(ctx context.Context, threadID string, sub *domainsubmission.Submission) (string, error)
	ClaudeClient                       func() QueueClaudeClient
	ClaudePrompt                       func(*domainsubmission.Submission) string
	AgentBinding                       func(chatType, chatID string) *routing.AgentBinding
	AgentBindingByID                   func(id string) *routing.AgentBinding
	ModelSettings                      ModelSettings
	BotProfile                         func() *routing.BotProfile
	PlanConfirmation                   interface {
		Invalidate(string) (*domaininteraction.PendingRequest, error)
	}
	PlanExpired func(context.Context, *domaininteraction.PendingRequest)
}

func runSessionAsync(deps Dependencies, sessionKey string, fn func()) {
	if fn == nil {
		return
	}
	deps.RunSessionAsync(strings.TrimSpace(sessionKey), fn)
}

type ModelSettings interface {
	SubmissionSnapshot(string, *conversation.Session, *domainsubmission.Submission) domainmodelconfig.Snapshot
}

func (s SubmissionQueueService) modelSnapshot(sess *conversation.Session, sub *domainsubmission.Submission) (domainmodelconfig.Snapshot, error) {
	if s.Deps.ModelSettings == nil {
		return domainmodelconfig.Snapshot{}, fmt.Errorf("model settings source is unavailable")
	}
	backend := domainmodelconfig.BackendCodex
	if s.Deps.Backend != nil {
		backend = s.Deps.Backend()
	}
	snapshot := s.Deps.ModelSettings.SubmissionSnapshot(backend, sess, sub)
	if !snapshot.Valid {
		return domainmodelconfig.Snapshot{}, fmt.Errorf("model settings snapshot is invalid")
	}
	return snapshot, nil
}

// ---------------------------------------------------------------------------
// Narrow provider interfaces
// ---------------------------------------------------------------------------

// QueueStateProvider narrows app state access.
type QueueStateProvider interface {
	Session(key string) *conversation.Session
	Submission(id string) *domainsubmission.Submission
	SaveSession(sess *conversation.Session) error
	CreateSubmission(sub *domainsubmission.Submission) (string, error)
	QueueSubmission(sessionKey, id string) error
	DequeueSubmission(sessionKey string) (string, error)
	MarkSubmissionRunning(id, threadID, turnID string) error
	FinalizeSubmission(id, status string) error
	UpdateSession(key string, mutate func(*conversation.Session)) (*conversation.Session, error)
	NextLocalID(prefix string) (string, error)
	UpdateSubmission(id string, mutate func(*domainsubmission.Submission)) error
	Sessions() []*conversation.Session
}

// QueueSkillResolver narrows skill resolution.
type QueueSkillResolver interface {
	ResolveSubmissionSkill(sessionKey, workspaceID, inputText string, attachments []domainsubmission.SubmissionAttachment) QueueSkillResolution
	SetSessionPendingSkill(sessionKey string, skill domainsubmission.SubmissionSkill)
	ClearSessionPendingSkill(sessionKey string)
}

// QueueSkillResolution describes how a submission's skill was resolved.
type QueueSkillResolution = skillapp.SubmissionSkillResolution

// QueueAttachmentResolver narrows inbound attachment resolution.
type QueueAttachmentResolver interface {
	ResolveInboundAttachments(msg *application.InboundMessage, workspaceID, sessionKey string) ([]domainsubmission.SubmissionAttachment, error)
}

// QueueLiveThreadProvider narrows live thread tracking.
type QueueLiveThreadProvider interface {
	MarkSessionThreadLive(sessionKey, threadID string)
	SessionHasLiveThread(sessionKey, threadID string) bool
	ClearSessionLiveThread(sessionKey string)
}

// QueuePendingQueueProvider narrows pending queue operations.
type QueuePendingQueueProvider interface {
	PendingInputSessionKey(msg *application.InboundMessage) string
	CollectPendingStagedImages(sessionKey, bucketSessionKey string) []conversation.SessionStagedImage
	ClearPendingStagedImages(sessionKey, bucketSessionKey string) error
}

// QueueRuntimeStateProvider narrows runtime state.
type QueueRuntimeStateProvider interface {
	NotePendingTurnBinding(threadID, sessionKey, submissionID string)
	ClearPendingTurnBindingForSubmission(threadID, submissionID string)
	BindTurnSubmission(threadID, turnID, sessionKey, submissionID string)
	MarkTurnStartedAt(turnID string, startedAt time.Time)
	ClearTurnBinding(turnID string)
	BoundSubmissionForTurn(turnID string) (string, *domainsubmission.Submission)
}

// QueueRuntimeMaintenanceProvider narrows runtime maintenance.
type QueueRuntimeMaintenanceProvider interface {
	CleanupSubmissionRuntimeState(sub *domainsubmission.Submission)
}

// QueueReplyContinuationProvider narrows reply continuation.
type QueueReplyContinuationProvider interface {
	RecordSubmissionSourceLinks(sub *domainsubmission.Submission)
	RecordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID string)
}

// QueueTurnStreamProvider narrows turn stream.
type QueueTurnStreamProvider interface {
	NoteTurnStarted(sessionKey string, sub *domainsubmission.Submission)
	DeleteTurnStream(turnID string)
}

// QueueAutoRetryProvider narrows auto retry.
type QueueAutoRetryProvider interface {
	ObserveAutoRetryTerminal(sessionKey, threadID, status string, sess *conversation.Session, sub *domainsubmission.Submission, reuseMessageID, lastError string) bool
	HasBlockingAutoRetry(sessionKey string) bool
}

// QueueBackendRuntimeProvider narrows backend runtime.
type QueueBackendRuntimeProvider interface {
	ReconcileCompletedTurnFromFinalOutput(sessionKey string, sess *conversation.Session) *conversation.Session
	DropThreadLineageAfterStartFailure(err error) bool
	DeferQueuedSubmissionsDuringRecovery() bool
}

// QueueClaudeClient narrows the Claude backend client for submission startup.
type QueueClaudeClient interface {
	EnsureSession(ctx context.Context, sessionKey string, ws *workspace.Workspace, resumeThreadID, model string) (string, error)
	StartTurn(ctx context.Context, sessionKey, threadID, turnID, prompt string) error
	StartSteerTurn(ctx context.Context, sessionKey, threadID, turnID, prompt, steerSubmissionID string) error
}

// QueueInflightMode represents the inflight mode for session submissions.
type QueueInflightMode = int

// Inflight mode constants.
const (
	InflightSingle     QueueInflightMode = 0
	InflightSerialized QueueInflightMode = 1
	InflightParallel   QueueInflightMode = 2
)

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// SubmissionQueueService manages submission queueing and dispatching.
type SubmissionQueueService struct {
	Deps Dependencies
}

// NewSubmissionQueueService creates a new SubmissionQueueService.
func NewSubmissionQueueService(deps Dependencies) SubmissionQueueService {
	return SubmissionQueueService{Deps: deps}
}

// EnqueueSubmission enqueues a new submission from an inbound message.
func (s SubmissionQueueService) EnqueueSubmission(msg *application.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
	a := s.Deps
	appState := a.AppState
	chatBinding := resolveAgentBinding(a, msg)
	newSession := func() *conversation.Session {
		bindingID := ""
		workspaceID := a.DefaultWorkspaceID()
		if chatBinding != nil {
			bindingID = strings.TrimSpace(chatBinding.ID)
			workspaceID = ""
		}
		return &conversation.Session{
			Key:           sessionKey,
			BindingID:     bindingID,
			WorkspaceID:   workspaceID,
			OwnerUserID:   conversationOwnerUserID(msg),
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID)),
			Status:        conversation.SessionStatusIdle.String(),
		}
	}
	sess := appState.Session(sessionKey)
	if sess == nil {
		sess = newSession()
	}
	if strings.EqualFold(strings.TrimSpace(sess.ChatType), "group") || (msg != nil && strings.EqualFold(strings.TrimSpace(msg.ChatType), "group")) {
		sess.OwnerUserID = ""
	}
	if strings.TrimSpace(sess.BindingID) == "" && chatBinding != nil {
		sess.BindingID = strings.TrimSpace(chatBinding.ID)
	}
	workspaceID := strings.TrimSpace(a.ResolveWorkspaceID(msg, sess, bindOnlyCurrentRoot))
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = firstNonEmpty(workspaceID, a.DefaultWorkspaceID())
	}
	if runtime := a.BackendRuntime; runtime != nil {
		sess = runtime.ReconcileCompletedTurnFromFinalOutput(sessionKey, sess)
	}
	if sess == nil {
		sess = appState.Session(sessionKey)
	}
	if sess == nil {
		sess = newSession()
		if strings.TrimSpace(sess.BindingID) == "" && chatBinding != nil {
			sess.BindingID = strings.TrimSpace(chatBinding.ID)
		}
	}
	if workspaceID == "" {
		workspaceID = firstNonEmpty(strings.TrimSpace(a.ResolveWorkspaceID(msg, sess, bindOnlyCurrentRoot)), strings.TrimSpace(sess.WorkspaceID))
		if strings.TrimSpace(sess.BindingID) == "" {
			workspaceID = firstNonEmpty(workspaceID, a.DefaultWorkspaceID())
		}
	}
	if workspaceID == "" {
		return fmt.Errorf("当前 Bot 在本群还没有配置工作区，请先使用 /workspace 选择、创建或 clone 工作区")
	}
	inboundAttachments, err := a.AttachmentResolver.ResolveInboundAttachments(msg, workspaceID, sessionKey)
	if err != nil {
		return err
	}
	pendingQueue := a.PendingQueue
	bucketSessionKey := pendingQueue.PendingInputSessionKey(msg)
	stagedImages := pendingQueue.CollectPendingStagedImages(sessionKey, bucketSessionKey)
	attachments := append(StagedImageAttachments(stagedImages), inboundAttachments...)
	skillResolution := a.SkillResolver.ResolveSubmissionSkill(sessionKey, workspaceID, msg.Text, attachments)
	if skillResolution.PendingReplacement != nil && strings.TrimSpace(skillResolution.InputText) == "" && len(attachments) == 0 {
		a.SkillResolver.SetSessionPendingSkill(sessionKey, *skillResolution.PendingReplacement)
		if err := a.ReplyText(a.context(), msg.MessageID, PendingConfirmationText(skillResolution.PendingReplacement.Name), a.ReplyInThreadEnabled(msg.ChatType)); err != nil {
			return err
		}
		return nil
	}
	sourceMessageIDs := UniqueStrings(append([]string{msg.MessageID}, StagedImageSourceMessageIDs(stagedImages)...))
	currentRootMessageID := firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))
	sourceRootMessageIDs := []string{currentRootMessageID}
	if !bindOnlyCurrentRoot {
		sourceRootMessageIDs = UniqueStrings(append(sourceRootMessageIDs, StagedImageRootMessageIDs(stagedImages)...))
	}
	mode := a.ConfiguredInflightMode()
	hasInFlight := conversation.HasInFlightSubmission(sess)
	queueLenBefore := len(sess.Queue)
	autoRetryBlocked := s.hasAutoRetryWorkAhead(appState, sess)
	serialBindingBlocked := s.hasSerialBindingWorkAhead(appState, sess)
	allowsAdditional := a.InflightAllowsAdditional(mode) && serialGroupExecutionKey(sess) == ""
	shouldAttemptStart := !autoRetryBlocked && !serialBindingBlocked && (!hasInFlight || allowsAdditional)
	willWaitInQueue := queueLenBefore > 0 || autoRetryBlocked || serialBindingBlocked || (hasInFlight && !allowsAdditional)
	if willWaitInQueue {
		sess.Status = conversation.SessionStatusQueued.String()
	}
	slog.Debug("submission enqueue begin",
		"session_key", sessionKey,
		"chat_id", msg.ChatID,
		"user_id", msg.UserID,
		"workspace_id", workspaceID,
		"active_thread_id", sess.ActiveThreadID,
		"active_turn_id", sess.ActiveTurnID,
		"queue_len", len(sess.Queue),
		"has_in_flight", hasInFlight,
		"active_operations_count", len(sess.ActiveOperations),
		"should_attempt_start", shouldAttemptStart,
		"will_wait_in_queue", willWaitInQueue,
		"auto_retry_blocked", autoRetryBlocked,
		"serial_binding_blocked", serialBindingBlocked,
	)
	if err := appState.SaveSession(sess); err != nil {
		return err
	}
	a.LogSessionState("submission enqueue session persisted", sessionKey, appState.Session(sessionKey))
	sub := &domainsubmission.Submission{
		SessionKey:           sessionKey,
		BindingID:            strings.TrimSpace(sess.BindingID),
		WorkspaceID:          workspaceID,
		UserID:               msg.UserID,
		ChatID:               msg.ChatID,
		TriggerMessageID:     msg.MessageID,
		SourceMessageIDs:     sourceMessageIDs,
		SourceRootMessageIDs: sourceRootMessageIDs,
		InputText:            skillResolution.InputText,
		Skills:               skillResolution.Skills,
		Attachments:          attachments,
		Status:               domainsubmission.SubmissionStatusQueued.String(),
		WaitedInQueue:        willWaitInQueue,
	}
	id, err := appState.CreateSubmission(sub)
	if err != nil {
		return err
	}
	if err := appState.QueueSubmission(sessionKey, id); err != nil {
		return err
	}
	if a.PlanConfirmation != nil {
		pending, err := a.PlanConfirmation.Invalidate(sessionKey)
		if err != nil {
			return err
		}
		if pending != nil && a.PlanExpired != nil {
			a.PlanExpired(a.context(), pending)
		}
	}
	sub.ID = id
	if len(stagedImages) > 0 {
		if err := pendingQueue.ClearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
			return err
		}
	}
	if skillResolution.ConsumePending {
		a.SkillResolver.ClearSessionPendingSkill(sessionKey)
	}
	slog.Debug("submission queued",
		"submission_id", id,
		"session_key", sessionKey,
		"active_thread_id", sess.ActiveThreadID,
		"active_turn_id", sess.ActiveTurnID,
	)
	a.LogSessionState("submission queued session snapshot", sessionKey, appState.Session(sessionKey))
	if shouldAttemptStart {
		slog.Debug("submission starting immediately",
			"submission_id", id,
			"session_key", sessionKey,
		)
		if err := s.StartNextSubmission(sessionKey); err != nil {
			return err
		}
		if !willWaitInQueue {
			return nil
		}
	}
	a.MarkSubmissionQueuedReactions(sub)
	a.SendQueuedNotice(a.context(), sub)
	if serialBindingBlocked && !autoRetryBlocked {
		runSessionAsync(a, sessionKey, func() {
			s.StartNextSubmissionAsync(sessionKey, "serialBindingQueued")
		})
	}
	return nil
}

func resolveAgentBinding(a Dependencies, msg *application.InboundMessage) *routing.AgentBinding {
	if msg == nil {
		return nil
	}
	if a.AgentBinding == nil {
		return nil
	}
	return a.AgentBinding(msg.ChatType, msg.ChatID)
}

func conversationOwnerUserID(msg *application.InboundMessage) string {
	if msg == nil || strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") {
		return ""
	}
	return strings.TrimSpace(msg.UserID)
}

func resolveAgentBindingByID(a Dependencies, id string) *routing.AgentBinding {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if a.AgentBindingByID == nil {
		return nil
	}
	return a.AgentBindingByID(id)
}

func submissionBinding(a Dependencies, sess *conversation.Session, sub *domainsubmission.Submission) *routing.AgentBinding {
	if sub != nil {
		if binding := resolveAgentBindingByID(a, sub.BindingID); binding != nil {
			return binding
		}
	}
	if sess != nil {
		return resolveAgentBindingByID(a, sess.BindingID)
	}
	return nil
}

func effectiveBindingApprovalPolicy(a Dependencies, sess *conversation.Session, sub *domainsubmission.Submission, ws *workspace.Workspace) string {
	if sess != nil && strings.TrimSpace(sess.ActiveThreadApprovalPolicy) != "" {
		return strings.TrimSpace(sess.ActiveThreadApprovalPolicy)
	}
	if binding := submissionBinding(a, sess, sub); binding != nil && strings.TrimSpace(binding.ApprovalPolicyOverride) != "" {
		return strings.TrimSpace(binding.ApprovalPolicyOverride)
	}
	if profile := submissionBotProfile(a); profile != nil && strings.TrimSpace(profile.ApprovalPolicy) != "" {
		return strings.TrimSpace(profile.ApprovalPolicy)
	}
	workspaceValue := ""
	if ws != nil {
		workspaceValue = ws.ApprovalPolicy
	}
	return conversation.EffectiveApprovalPolicy(sess, workspaceValue)
}

func effectiveBindingSandboxMode(a Dependencies, sess *conversation.Session, sub *domainsubmission.Submission, ws *workspace.Workspace) string {
	if sess != nil && strings.TrimSpace(sess.ActiveThreadSandboxMode) != "" {
		return strings.TrimSpace(sess.ActiveThreadSandboxMode)
	}
	if binding := submissionBinding(a, sess, sub); binding != nil && strings.TrimSpace(binding.SandboxModeOverride) != "" {
		return strings.TrimSpace(binding.SandboxModeOverride)
	}
	if profile := submissionBotProfile(a); profile != nil && strings.TrimSpace(profile.SandboxMode) != "" {
		return strings.TrimSpace(profile.SandboxMode)
	}
	workspaceValue := ""
	if ws != nil {
		workspaceValue = ws.SandboxMode
	}
	return conversation.EffectiveSandboxMode(sess, workspaceValue)
}

func effectiveBindingServiceTier(a Dependencies, sess *conversation.Session, sub *domainsubmission.Submission) string {
	if value := conversation.EffectiveServiceTier(sess); strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if binding := submissionBinding(a, sess, sub); binding != nil {
		if value := strings.TrimSpace(binding.ServiceTierOverride); value != "" {
			return value
		}
	}
	return strings.TrimSpace(botProfileServiceTier(a))
}

func effectiveBindingMultiAgentMode(a Dependencies, sess *conversation.Session, sub *domainsubmission.Submission, ws *workspace.Workspace) string {
	if sess != nil && strings.TrimSpace(sess.ActiveThreadMultiAgentMode) != "" {
		return strings.TrimSpace(sess.ActiveThreadMultiAgentMode)
	}
	if binding := submissionBinding(a, sess, sub); binding != nil && strings.TrimSpace(binding.MultiAgentModeOverride) != "" {
		return strings.TrimSpace(binding.MultiAgentModeOverride)
	}
	if profile := submissionBotProfile(a); profile != nil && strings.TrimSpace(profile.MultiAgentMode) != "" {
		return strings.TrimSpace(profile.MultiAgentMode)
	}
	workspaceValue := ""
	if ws != nil {
		workspaceValue = ws.MultiAgentMode
	}
	return conversation.EffectiveMultiAgentMode(sess, workspaceValue)
}

func submissionBotProfile(a Dependencies) *routing.BotProfile {
	if a.BotProfile == nil {
		return nil
	}
	return a.BotProfile()
}

func botProfileServiceTier(a Dependencies) string {
	if profile := submissionBotProfile(a); profile != nil {
		return strings.TrimSpace(profile.ServiceTier)
	}
	return ""
}

// PendingConfirmationText returns the pending confirmation text for a skill.
func PendingConfirmationText(name string) string {
	return fmt.Sprintf("已识别到技能 `%s`，请确认是否使用。", strings.TrimSpace(name))
}

// StartNextSubmission starts the next queued submission for a session.
func (s SubmissionQueueService) StartNextSubmission(sessionKey string) error {
	return s.StartNextSubmissionWithFailureNotice(sessionKey, false)
}

// StartNextSubmissionWithFailureNotice starts the next queued submission,
// optionally notifying on failure.
func (s SubmissionQueueService) StartNextSubmissionWithFailureNotice(sessionKey string, notifyFailure bool) error {
	a := s.Deps
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}
	if !a.TryBeginStart(sessionKey) {
		slog.Debug("startNextSubmission coalesced",
			"session_key", sessionKey,
		)
		return nil
	}
	defer func() {
		if a.FinishStart(sessionKey) {
			runSessionAsync(a, sessionKey, func() {
				s.StartNextSubmissionAsync(sessionKey, "coalesced")
			})
		}
	}()

	appState := a.AppState
	for {
		sess := appState.Session(sessionKey)
		a.LogSessionState("startNextSubmission entry", sessionKey, sess)
		if sess == nil {
			slog.Debug("startNextSubmission skipped", "session_key", sessionKey, "has_session", false)
			return nil
		}
		if s.hasAutoRetryWorkAhead(appState, sess) {
			slog.Debug("startNextSubmission skipped",
				"session_key", sessionKey,
				"reason", "auto_retry_priority",
				"group_execution_key", serialGroupExecutionKey(sess),
			)
			return nil
		}
		nextMode := a.ConfiguredInflightMode()
		if len(sess.Queue) > 0 {
			if nextSub := appState.Submission(sess.Queue[0]); nextSub != nil {
				nextMode = a.ConfiguredInflightMode()
			}
		}
		if conversation.HasInFlightSubmission(sess) && !a.InflightAllowsAdditional(nextMode) {
			slog.Debug("startNextSubmission skipped",
				"session_key", sessionKey,
				"has_session", true,
				"active_turn_id", sess.ActiveTurnID,
			)
			return nil
		}
		if s.hasSerialBindingWorkAhead(appState, sess) {
			slog.Debug("startNextSubmission skipped",
				"session_key", sessionKey,
				"reason", "serial_binding_work_ahead",
				"group_execution_key", serialGroupExecutionKey(sess),
			)
			return nil
		}
		if runtime := a.BackendRuntime; runtime != nil && runtime.DeferQueuedSubmissionsDuringRecovery() {
			slog.Debug("startNextSubmission deferred",
				"session_key", sessionKey,
				"reason", "codex_runtime_recovering",
			)
			return nil
		}
		subID, err := appState.DequeueSubmission(sessionKey)
		if err != nil || subID == "" {
			slog.Debug("startNextSubmission no queued item",
				"session_key", sessionKey,
				"error", err,
			)
			if err == nil {
				updatedSess, updateErr := appState.UpdateSession(sessionKey, func(current *conversation.Session) {
					if current == nil {
						return
					}
					conversation.RefreshPendingStatus(current)
				})
				if updateErr != nil {
					return updateErr
				}
				a.LogSessionState("startNextSubmission empty-after-dequeue", sessionKey, updatedSess)
			} else {
				a.LogSessionState("startNextSubmission empty-after-dequeue", sessionKey, appState.Session(sessionKey))
			}
			return err
		}
		a.LogSessionState("startNextSubmission after dequeue", sessionKey, appState.Session(sessionKey))
		sub := appState.Submission(subID)
		if sub == nil {
			slog.Warn("queued submission missing",
				"session_key", sessionKey,
				"submission_id", subID,
			)
			updatedSess, updateErr := appState.UpdateSession(sessionKey, func(current *conversation.Session) {
				if current == nil {
					return
				}
				conversation.RefreshPendingStatus(current)
			})
			if updateErr != nil {
				return updateErr
			}
			a.LogSessionState("startNextSubmission after missing queued item", sessionKey, updatedSess)
			if updatedSess != nil && !conversation.HasInFlightSubmission(updatedSess) && len(updatedSess.Queue) > 0 {
				continue
			}
			return nil
		}
		ws := a.Workspace(sub.WorkspaceID)
		if ws == nil {
			slog.Error("workspace resolution failed",
				"submission_id", sub.ID,
				"workspace_id", sub.WorkspaceID,
				"default_workspace_id", a.DefaultWorkspaceID(),
			)
			return fmt.Errorf("workspace %q not found", sub.WorkspaceID)
		}
		sess = appState.Session(sessionKey)
		if sess == nil {
			return fmt.Errorf("session %q disappeared after dequeue", sessionKey)
		}
		slog.Debug("startNextSubmission picked",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"cwd", ws.Cwd,
			"thread_id", sess.ActiveThreadID,
		)
		if a.Backend != nil && a.Backend() == "claude" {
			return s.StartNextClaudeSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
		}
		return s.StartNextCodexSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
	}
}

// HandleSubmissionStartFailure handles a submission start failure.
func (s SubmissionQueueService) HandleSubmissionStartFailure(sessionKey, threadID string, sub *domainsubmission.Submission, err error, notifyFailure bool) {
	a := s.Deps
	appState := a.AppState
	dropThreadLineage := false
	if runtime := a.BackendRuntime; runtime != nil {
		dropThreadLineage = runtime.DropThreadLineageAfterStartFailure(err)
	}
	if sub != nil {
		current := appState.Submission(sub.ID)
		switch {
		case current == nil:
			notifyFailure = false
		case current.Finalized:
			notifyFailure = false
			sub = current
		default:
			sub = current
		}
	}
	if sub != nil {
		a.RuntimeState.ClearPendingTurnBindingForSubmission(threadID, sub.ID)
	}
	a.ClearSubmissionProcessingReactions(sub)
	if sub != nil {
		_ = appState.FinalizeSubmission(sub.ID, domainsubmission.SubmissionStatusFailed.String())
	}
	shouldStartNext := false
	clearedThreadLineage := false
	if sess, saveErr := appState.UpdateSession(sessionKey, func(sess *conversation.Session) {
		if sess == nil {
			return
		}
		if sub != nil {
			conversation.RemoveActiveOperation(sess, sub.ID, "")
		}
		if dropThreadLineage && strings.TrimSpace(threadID) != "" && strings.TrimSpace(sess.ActiveThreadID) == strings.TrimSpace(threadID) {
			conversation.ClearThreadContext(sess)
			conversation.ClearBackendThread(sess, "codex")
			clearedThreadLineage = true
		}
		if !conversation.HasActiveOperations(sess) {
			if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
				sess.Status = conversation.SessionStatusQueued.String()
			} else {
				sess.Status = conversation.SessionStatusIdle.String()
			}
		}
	}); saveErr != nil {
		slog.Error("submission start failure session cleanup failed",
			"session_key", sessionKey,
			"submission_id", func() string {
				if sub == nil {
					return ""
				}
				return sub.ID
			}(),
			"thread_id", threadID,
			"error", saveErr,
		)
	} else if sess != nil {
		retryPending := a.AutoRetry.ObserveAutoRetryTerminal(sessionKey, threadID, "failed", sess, sub, "", err.Error())
		shouldStartNext = !retryPending && s.NextQueuedSessionKey(sessionKey) != ""
	}
	if clearedThreadLineage {
		a.LiveThread.ClearSessionLiveThread(sessionKey)
	}
	if notifyFailure && sub != nil {
		willContinue := shouldStartNext
		s.NotifySubmissionStartFailure(a.context(), sub, err, willContinue)
	}
	a.RuntimeMaintenance.CleanupSubmissionRuntimeState(sub)
	if shouldStartNext {
		nextSessionKey := s.NextQueuedSessionKey(sessionKey)
		runSessionAsync(a, firstNonEmpty(nextSessionKey, sessionKey), func() {
			s.StartNextSubmissionAsync(firstNonEmpty(nextSessionKey, sessionKey), "turnStartFailed")
		})
	}
}

// NotifySubmissionStartFailure sends a failure notification.
func (s SubmissionQueueService) NotifySubmissionStartFailure(ctx context.Context, sub *domainsubmission.Submission, err error, willContinue bool) {
	a := s.Deps
	if sub == nil || err == nil {
		return
	}
	body := "任务启动失败: " + strings.TrimSpace(err.Error())
	if willContinue {
		body += "\n\n本条消息已跳过，正在继续处理后续排队消息。"
	} else {
		body += "\n\n本条消息未开始执行，可稍后重试。"
	}
	inThread := a.ReplyInThreadForSubmission(sub)
	if strings.TrimSpace(sub.TriggerMessageID) != "" {
		if replyErr := a.ReplyText(ctx, sub.TriggerMessageID, body, inThread); replyErr == nil {
			return
		}
	}
	_ = inThread
	if strings.TrimSpace(sub.ChatID) != "" {
		_ = a.ReplyText(ctx, sub.ChatID, body, false)
	}
}

// StartNextSubmissionAsync asynchronously starts the next submission if needed.
func (s SubmissionQueueService) StartNextSubmissionAsync(sessionKey, source string) {
	a := s.Deps
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	nextSessionKey := s.NextQueuedSessionKey(sessionKey)
	if nextSessionKey == "" {
		return
	}
	if err := s.StartNextSubmissionWithFailureNotice(nextSessionKey, true); err != nil {
		slog.Error("async startNextSubmission failed",
			"session_key", nextSessionKey,
			"source_session_key", sessionKey,
			"source", source,
			"error", err,
		)
		a.LogSessionState("async startNextSubmission failed snapshot", nextSessionKey, a.AppState.Session(nextSessionKey))
	}
}

// NextQueuedSessionKey returns the next session that may start work for the
// same local execution surface as sessionKey. Group bindings are serialized
// across roots; p2p and unbound sessions keep the existing per-session queue.
func (s SubmissionQueueService) NextQueuedSessionKey(sessionKey string) string {
	appState := s.Deps.AppState
	sess := appState.Session(sessionKey)
	if sess == nil {
		return ""
	}
	if s.hasAutoRetryWorkAhead(appState, sess) {
		return ""
	}
	if groupExecutionKey := serialGroupExecutionKey(sess); groupExecutionKey != "" {
		return nextQueuedSerialSessionKey(appState, groupExecutionKey)
	}
	if conversation.ShouldStartNextSubmission(sess) {
		return strings.TrimSpace(sess.Key)
	}
	return ""
}

func (s SubmissionQueueService) hasAutoRetryWorkAhead(appState QueueStateProvider, sess *conversation.Session) bool {
	if sess == nil {
		return false
	}
	autoRetry := s.Deps.AutoRetry
	if autoRetry == nil {
		return false
	}
	groupExecutionKey := serialGroupExecutionKey(sess)
	if groupExecutionKey == "" {
		return autoRetry.HasBlockingAutoRetry(strings.TrimSpace(sess.Key))
	}
	for _, candidate := range appState.Sessions() {
		if candidate == nil || serialGroupExecutionKey(candidate) != groupExecutionKey {
			continue
		}
		if autoRetry.HasBlockingAutoRetry(strings.TrimSpace(candidate.Key)) {
			return true
		}
	}
	return autoRetry.HasBlockingAutoRetry(strings.TrimSpace(sess.Key))
}

func (s SubmissionQueueService) hasSerialBindingWorkAhead(appState QueueStateProvider, sess *conversation.Session) bool {
	groupExecutionKey := serialGroupExecutionKey(sess)
	if groupExecutionKey == "" {
		return false
	}
	currentKey := strings.TrimSpace(sess.Key)
	currentHead := queuedHeadSubmission(appState, sess)
	for _, candidate := range appState.Sessions() {
		if candidate == nil || strings.TrimSpace(candidate.Key) == currentKey || serialGroupExecutionKey(candidate) != groupExecutionKey {
			continue
		}
		if conversation.HasInFlightSubmission(candidate) {
			return true
		}
		candidateHead := queuedHeadSubmission(appState, candidate)
		if candidateHead == nil {
			continue
		}
		if currentHead == nil || submissionBefore(candidateHead, currentHead) {
			return true
		}
	}
	return false
}

func nextQueuedSerialSessionKey(appState QueueStateProvider, groupExecutionKey string) string {
	groupExecutionKey = strings.TrimSpace(groupExecutionKey)
	if groupExecutionKey == "" {
		return ""
	}
	var bestSessionKey string
	var bestSub *domainsubmission.Submission
	for _, sess := range appState.Sessions() {
		if sess == nil || serialGroupExecutionKey(sess) != groupExecutionKey {
			continue
		}
		if conversation.HasInFlightSubmission(sess) {
			return ""
		}
		head := queuedHeadSubmission(appState, sess)
		if head == nil {
			continue
		}
		if bestSub == nil || submissionBefore(head, bestSub) {
			bestSub = head
			bestSessionKey = strings.TrimSpace(sess.Key)
		}
	}
	return bestSessionKey
}

func serialGroupExecutionKey(sess *conversation.Session) string {
	if sess == nil || !strings.EqualFold(strings.TrimSpace(sess.ChatType), "group") {
		return ""
	}
	frontendID, keyChatID, ok := sessionGroupKeyParts(sess.Key)
	if !ok && strings.TrimSpace(sess.BindingID) == "" {
		return ""
	}
	chatID := firstNonEmpty(keyChatID, strings.TrimSpace(sess.ChatID))
	if chatID == "" {
		return ""
	}
	if ok && frontendID == "" {
		return "group:" + chatID
	}
	if !ok {
		return "binding:" + strings.TrimSpace(sess.BindingID)
	}
	return "frontend:" + frontendID + ":group:" + chatID
}

func sessionGroupKeyParts(sessionKey string) (frontendID, chatID string, ok bool) {
	frontendID, chatType, chatID, _, _ := identity.ParseSessionKey(sessionKey)
	if strings.TrimSpace(chatID) == "" {
		return "", "", false
	}
	if chatType != "" && chatType != "group" {
		return "", "", false
	}
	return strings.TrimSpace(frontendID), strings.TrimSpace(chatID), true
}

func queuedHeadSubmission(appState QueueStateProvider, sess *conversation.Session) *domainsubmission.Submission {
	if sess == nil || len(sess.Queue) == 0 {
		return nil
	}
	id := strings.TrimSpace(sess.Queue[0])
	if id == "" {
		return nil
	}
	if sub := appState.Submission(id); sub != nil {
		return sub
	}
	return &domainsubmission.Submission{ID: id, SessionKey: strings.TrimSpace(sess.Key)}
}

func submissionBefore(a, b *domainsubmission.Submission) bool {
	if b == nil {
		return a != nil
	}
	if a == nil {
		return false
	}
	if a.CreatedAt != 0 || b.CreatedAt != 0 {
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt < b.CreatedAt
		}
	}
	return strings.TrimSpace(a.ID) < strings.TrimSpace(b.ID)
}

// StartNextCodexSubmissionWithFailureNotice handles Codex-specific submission
// startup: thread creation, turn start, and state binding.
func (s SubmissionQueueService) StartNextCodexSubmissionWithFailureNotice(sessionKey string, sess *conversation.Session, sub *domainsubmission.Submission, ws *workspace.Workspace, notifyFailure bool) error {
	a := s.Deps
	appState := a.AppState
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	if !(sub != nil && conversation.CanResumeThreadForWorkspace(sess, sub.WorkspaceID)) {
		threadID = ""
		conversation.ClearThreadContext(sess)
	}
	if threadID != "" && !a.LiveThread.SessionHasLiveThread(sessionKey, threadID) {
		slog.Warn("dropping non-live session thread before submission",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"workspace_id", sub.WorkspaceID,
		)
		threadID = ""
		conversation.ClearThreadContext(sess)
	}
	snapshot, err := s.modelSnapshot(sess, sub)
	if err != nil {
		return err
	}
	sub.ModelConfig = snapshot
	if err := appState.UpdateSubmission(sub.ID, func(current *domainsubmission.Submission) { current.ModelConfig = snapshot }); err != nil {
		return err
	}
	effectiveModel, effectiveReasoningEffort := sub.ModelConfig.Model, sub.ModelConfig.Effort
	effectiveApprovalPolicy := effectiveBindingApprovalPolicy(a, sess, sub, ws)
	effectiveSandboxMode := effectiveBindingSandboxMode(a, sess, sub, ws)
	effectiveServiceTier := effectiveBindingServiceTier(a, sess, sub)
	effectiveMultiAgentMode := effectiveBindingMultiAgentMode(a, sess, sub, ws)
	createdThread := threadID == ""
	if threadID == "" {
		slog.Debug("thread start request",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"cwd", ws.Cwd,
			"model", effectiveModel,
		)
		threadCtx, threadCancel := context.WithTimeout(a.context(), 30*time.Second)
		threadResp, err := a.StartConversation(threadCtx, ws, sess, sub, effectiveModel)
		threadCancel()
		if err != nil {
			s.HandleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
			slog.Error("thread/start failed",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"workspace_id", sub.WorkspaceID,
				"cwd", ws.Cwd,
				"error", err,
			)
			a.LogSessionState("startNextSubmission thread-start-failed", sessionKey, appState.Session(sessionKey))
			return err
		}
		threadID = threadResp.ID
		slog.Debug("thread started",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"model", effectiveModel,
		)
		conversation.SetThreadContext(sess, sub.WorkspaceID, threadID, threadResp.Name, threadResp.Preview)
		a.LiveThread.MarkSessionThreadLive(sessionKey, threadID)
	}
	if threadID != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
		conversation.SetThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	}
	if threadID != "" {
		a.LiveThread.MarkSessionThreadLive(sessionKey, threadID)
	}
	conversation.UpsertActiveOperation(sess, conversation.SessionActiveOperation{
		Kind:         conversation.OpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
	})
	sess.Status = conversation.SessionStatusTurnStarting.String()
	sub.ThreadID = threadID
	sub.Status = domainsubmission.SubmissionStatusRunning.String()
	a.RuntimeState.NotePendingTurnBinding(threadID, sessionKey, sub.ID)
	if err := appState.SaveSession(sess); err != nil {
		a.RuntimeState.ClearPendingTurnBindingForSubmission(threadID, sub.ID)
		return err
	}
	if err := appState.MarkSubmissionRunning(sub.ID, threadID, ""); err != nil {
		a.RuntimeState.ClearPendingTurnBindingForSubmission(threadID, sub.ID)
		return err
	}
	a.MarkSubmissionRunningReactions(sub)
	a.LogSessionState("startNextSubmission session starting", sessionKey, appState.Session(sessionKey))
	turnCtx, turnCancel := context.WithTimeout(a.context(), 30*time.Second)
	turnID := ""
	var turnErr error
	if a.IsReviewSubmission(sub) {
		turnID, turnErr = a.StartSubmissionReview(turnCtx, threadID, sub)
	} else {
		turnID, turnErr = a.StartSubmissionTurn(turnCtx, sessionKey, threadID, sub, ws.Cwd, effectiveApprovalPolicy, effectiveSandboxMode, effectiveServiceTier, effectiveModel, effectiveReasoningEffort, effectiveMultiAgentMode)
	}
	turnCancel()
	if turnErr == nil && sub.ModelConfig.Valid {
		applied := sub.ModelConfig
		// Thread initialization settings are unchanged for an already-live thread.
		if sess.AppliedModelConfig.Valid && sess.AppliedModelConfig.Backend == "codex" && !createdThread {
			applied.ReviewModel = sess.AppliedModelConfig.ReviewModel
			applied.SubagentModel = sess.AppliedModelConfig.SubagentModel
			applied.SubagentEffort = sess.AppliedModelConfig.SubagentEffort
		}
		sess.AppliedModelConfig = applied
		sess.ModelConfigError = ""
	}

	if turnErr != nil {
		if errors.Is(turnErr, context.DeadlineExceeded) {
			slog.Warn("turn start timed out; waiting for delayed notification",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"thread_id", threadID,
				"workspace_id", sub.WorkspaceID,
			)
			a.LogSessionState("startNextSubmission awaiting turn-start-notification", sessionKey, appState.Session(sessionKey))
			return nil
		}
		s.HandleSubmissionStartFailure(sessionKey, threadID, sub, turnErr, notifyFailure)
		slog.Error("turn start chain failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"workspace_id", sub.WorkspaceID,
			"error", turnErr,
		)
		a.LogSessionState("startNextSubmission turn-start-failed", sessionKey, appState.Session(sessionKey))
		return turnErr
	}
	slog.Debug("turn started",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	conversation.UpsertActiveOperation(sess, conversation.SessionActiveOperation{
		Kind:         conversation.OpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
		TurnID:       turnID,
	})
	sess.Status = conversation.SessionStatusTurnInProgress.String()
	a.RuntimeState.BindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	a.RuntimeState.MarkTurnStartedAt(turnID, time.Now())
	a.RuntimeState.ClearPendingTurnBindingForSubmission(threadID, sub.ID)
	sub.ThreadID = threadID
	sub.TurnID = turnID
	sub.Status = domainsubmission.SubmissionStatusRunning.String()
	if err := appState.SaveSession(sess); err != nil {
		return err
	}
	a.LogSessionState("startNextSubmission session activated", sessionKey, appState.Session(sessionKey))
	if err := appState.MarkSubmissionRunning(sub.ID, threadID, turnID); err != nil {
		return err
	}
	a.ReplyContinuation.RecordSubmissionSourceLinks(sub)
	recordLegacySessionRootTurnBinding(a.ReplyContinuation, sess, sub, sessionKey, threadID, turnID)
	a.TurnStream.NoteTurnStarted(sessionKey, sub)
	slog.Debug("startNextSubmission completed",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	return nil
}

// firstNonEmpty returns the first non-empty trimmed string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func recordLegacySessionRootTurnBinding(reply QueueReplyContinuationProvider, sess *conversation.Session, sub *domainsubmission.Submission, sessionKey, threadID, turnID string) {
	if reply == nil || sess == nil || sub.HasSourceRootMessages() {
		return
	}
	reply.RecordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
}

func (d Dependencies) context() context.Context {
	if d.Context != nil {
		return d.Context()
	}
	return context.Background()
}

// ConversationStarted is the semantic result of starting a backend conversation.
type ConversationStarted struct{ ID, Name, Preview string }

func (s SubmissionQueueService) StartQueuedSubmission(key string, sess *conversation.Session, sub *domainsubmission.Submission, ws *workspace.Workspace, notify bool) error {
	if s.Deps.Backend != nil && s.Deps.Backend() == "claude" {
		return s.StartNextClaudeSubmissionWithFailureNotice(key, sess, sub, ws, notify)
	}
	return s.StartNextCodexSubmissionWithFailureNotice(key, sess, sub, ws, notify)
}
