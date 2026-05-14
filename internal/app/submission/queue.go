// Package submission provides services for submission queueing, pending
// queue management, and related pure helpers. These services have no
// dependency on *App; they communicate with the host through narrow
// provider interfaces injected at construction time.
package submission

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/app/appcore"
	"feidex/internal/app/sessionctx"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// App interface — what the service needs from the host application
// ---------------------------------------------------------------------------

// App defines the interface the submission queue service requires from the
// host application.
type App interface {
	// Provider accessors — return narrow interfaces for each dependency.
	SubmissionQueueAppState() QueueAppStateProvider
	SubmissionQueueSkillResolver() QueueSkillResolver
	SubmissionQueueAttachmentResolver() QueueAttachmentResolver
	SubmissionQueueLiveThread() QueueLiveThreadProvider
	SubmissionQueuePendingQueue() QueuePendingQueueProvider
	SubmissionQueueRuntimeState() QueueRuntimeStateProvider
	SubmissionQueueRuntimeMaintenance() QueueRuntimeMaintenanceProvider
	SubmissionQueueReplyContinuation() QueueReplyContinuationProvider
	SubmissionQueueTurnStream() QueueTurnStreamProvider
	SubmissionQueueAutoRetry() QueueAutoRetryProvider
	SubmissionQueueConversationBackend() QueueConversationBackendProvider
	SubmissionQueueBackendRuntime() QueueBackendRuntimeProvider

	// Direct app methods for simple operations.
	SubmissionQueueDefaultWorkspaceID() string
	SubmissionQueueWorkspace(id string) *config.Workspace
	SubmissionQueueReplyInThreadEnabled(chatType string) bool
	SubmissionQueueReplyInThreadForSubmission(sub *state.Submission) bool
	SubmissionQueueConfiguredInflightMode() QueueInflightMode
	SubmissionQueueInflightAllowsAdditional(mode QueueInflightMode) bool
	SubmissionQueueResolveWorkspaceID(msg *feishu.InboundMessage, sess *state.Session, bindOnlyCurrentRoot bool) string
	SubmissionQueueReplyText(ctx context.Context, messageID, text string, inThread bool) error
	SubmissionQueueSendQueuedNotice(ctx context.Context, sub *state.Submission)
	SubmissionQueueSendStartFailureNotice(ctx context.Context, sub *state.Submission, err error, willContinue bool)
	SubmissionQueueRunAsync(fn func())
	SubmissionQueueTryBeginStart(sessionKey string) bool
	SubmissionQueueFinishStart(sessionKey string) bool
	SubmissionQueueLogSessionState(event, sessionKey string, sess *state.Session)

	// Reactions (delegated to PendingQueueService through app).
	SubmissionQueueMarkSubmissionQueuedReactions(sub *state.Submission)
	SubmissionQueueMarkSubmissionRunningReactions(sub *state.Submission)
	SubmissionQueueClearSubmissionProcessingReactions(sub *state.Submission)

	// Backend-specific start hooks.
	SubmissionQueueIsReviewSubmission(sub *state.Submission) bool
	SubmissionQueueStartSubmissionTurn(ctx context.Context, sessionKey, threadID string, sub *state.Submission, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort string) (string, error)
	SubmissionQueueStartSubmissionReview(ctx context.Context, threadID string, sub *state.Submission) (string, error)
	SubmissionQueueBuildThreadStartParams(ws *config.Workspace, sess *state.Session, model string) codexrpc.ThreadStartParams
	SubmissionQueueRequireCodexClient() (appcore.CodexClient, error)
	SubmissionQueueClaudeClient() QueueClaudeClient
	SubmissionQueueConfiguredClaudeModel() string
}

// ---------------------------------------------------------------------------
// Narrow provider interfaces
// ---------------------------------------------------------------------------

// QueueAppStateProvider narrows app state access.
type QueueAppStateProvider interface {
	Session(key string) *state.Session
	Submission(id string) *state.Submission
	SaveSession(sess *state.Session) error
	CreateSubmission(sub *state.Submission) (string, error)
	QueueSubmission(sessionKey, id string) error
	DequeueSubmission(sessionKey string) (string, error)
	MarkSubmissionRunning(id, threadID, turnID string) error
	FinalizeSubmission(id, status string) error
	UpdateSession(key string, mutate func(*state.Session)) (*state.Session, error)
	NextLocalID(prefix string) (string, error)
	DeletePendingRequests(match func(*state.PendingRequest) bool)
	DeleteMessageLinks(match func(*state.MessageLink) bool)
	UpdateSubmission(id string, mutate func(*state.Submission)) error
	Sessions() []*state.Session
}

// QueueSkillResolver narrows skill resolution.
type QueueSkillResolver interface {
	ResolveSubmissionSkill(sessionKey, workspaceID, inputText string, attachments []state.SubmissionAttachment) QueueSkillResolution
	SetSessionPendingSkill(sessionKey string, skill state.SubmissionSkill)
	ClearSessionPendingSkill(sessionKey string)
}

// QueueSkillResolution describes how a submission's skill was resolved.
type QueueSkillResolution struct {
	InputText          string
	Skills             []state.SubmissionSkill
	ConsumePending     bool
	PendingReplacement *state.SubmissionSkill
}

// QueueAttachmentResolver narrows inbound attachment resolution.
type QueueAttachmentResolver interface {
	ResolveInboundAttachments(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]state.SubmissionAttachment, error)
}

// QueueLiveThreadProvider narrows live thread tracking.
type QueueLiveThreadProvider interface {
	MarkSessionThreadLive(sessionKey, threadID string)
	SessionHasLiveThread(sessionKey, threadID string) bool
	ClearSessionLiveThread(sessionKey string)
}

// QueuePendingQueueProvider narrows pending queue operations.
type QueuePendingQueueProvider interface {
	PendingInputSessionKey(msg *feishu.InboundMessage) string
	CollectPendingStagedImages(sessionKey, bucketSessionKey string) []state.SessionStagedImage
	ClearPendingStagedImages(sessionKey, bucketSessionKey string) error
}

// QueueRuntimeStateProvider narrows runtime state.
type QueueRuntimeStateProvider interface {
	NotePendingTurnBinding(threadID, sessionKey, submissionID string)
	ClearPendingTurnBindingForSubmission(threadID, submissionID string)
	BindTurnSubmission(threadID, turnID, sessionKey, submissionID string)
	MarkTurnStartedAt(turnID string, startedAt time.Time)
	ClearTurnBinding(turnID string)
	ClearTurnItemStates(turnID string)
	BoundSubmissionForTurn(turnID string) (string, *state.Submission)
}

// QueueRuntimeMaintenanceProvider narrows runtime maintenance.
type QueueRuntimeMaintenanceProvider interface {
	CleanupSubmissionRuntimeState(sub *state.Submission)
}

// QueueReplyContinuationProvider narrows reply continuation.
type QueueReplyContinuationProvider interface {
	RecordSubmissionSourceLinks(sub *state.Submission)
	RecordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID string)
}

// QueueTurnStreamProvider narrows turn stream.
type QueueTurnStreamProvider interface {
	NoteTurnStarted(sessionKey string, sub *state.Submission)
	DeleteTurnStream(turnID string)
}

// QueueAutoRetryProvider narrows auto retry.
type QueueAutoRetryProvider interface {
	ObserveAutoRetryTerminal(sessionKey, threadID, status string, sess *state.Session, sub *state.Submission, reuseMessageID string) bool
}

// QueueConversationBackendProvider narrows conversation backend.
type QueueConversationBackendProvider interface {
	StartQueuedSubmission(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error
}

// QueueBackendRuntimeProvider narrows backend runtime.
type QueueBackendRuntimeProvider interface {
	ReconcileCompletedTurnFromFinalOutput(sessionKey string, sess *state.Session) *state.Session
	DropThreadLineageAfterStartFailure(err error) bool
	DeferQueuedSubmissionsDuringRecovery() bool
}

// QueueClaudeClient narrows the Claude backend client for submission startup.
type QueueClaudeClient interface {
	EnsureSession(ctx context.Context, sessionKey string, ws *config.Workspace, resumeThreadID, model string) (string, error)
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
	App App
}

// NewSubmissionQueueService creates a new SubmissionQueueService.
func NewSubmissionQueueService(app App) SubmissionQueueService {
	return SubmissionQueueService{App: app}
}

// EnqueueSubmission enqueues a new submission from an inbound message.
func (s SubmissionQueueService) EnqueueSubmission(msg *feishu.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
	a := s.App
	appState := a.SubmissionQueueAppState()
	sess := appState.Session(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   a.SubmissionQueueDefaultWorkspaceID(),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        state.SessionStatusIdle.String(),
		}
	}
	workspaceID := strings.TrimSpace(a.SubmissionQueueResolveWorkspaceID(msg, sess, bindOnlyCurrentRoot))
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = firstNonEmpty(workspaceID, a.SubmissionQueueDefaultWorkspaceID())
	}
	if runtime := a.SubmissionQueueBackendRuntime(); runtime != nil {
		sess = runtime.ReconcileCompletedTurnFromFinalOutput(sessionKey, sess)
	}
	if sess == nil {
		sess = appState.Session(sessionKey)
	}
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   a.SubmissionQueueDefaultWorkspaceID(),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        state.SessionStatusIdle.String(),
		}
	}
	if workspaceID == "" {
		workspaceID = firstNonEmpty(strings.TrimSpace(a.SubmissionQueueResolveWorkspaceID(msg, sess, bindOnlyCurrentRoot)), strings.TrimSpace(sess.WorkspaceID), a.SubmissionQueueDefaultWorkspaceID())
	}
	inboundAttachments, err := a.SubmissionQueueAttachmentResolver().ResolveInboundAttachments(msg, workspaceID, sessionKey)
	if err != nil {
		return err
	}
	pendingQueue := a.SubmissionQueuePendingQueue()
	bucketSessionKey := pendingQueue.PendingInputSessionKey(msg)
	stagedImages := pendingQueue.CollectPendingStagedImages(sessionKey, bucketSessionKey)
	attachments := append(StagedImageAttachments(stagedImages), inboundAttachments...)
	skillResolution := a.SubmissionQueueSkillResolver().ResolveSubmissionSkill(sessionKey, workspaceID, msg.Text, attachments)
	if skillResolution.PendingReplacement != nil && strings.TrimSpace(skillResolution.InputText) == "" && len(attachments) == 0 {
		a.SubmissionQueueSkillResolver().SetSessionPendingSkill(sessionKey, *skillResolution.PendingReplacement)
		if err := a.SubmissionQueueReplyText(context.Background(), msg.MessageID, PendingConfirmationText(skillResolution.PendingReplacement.Name), a.SubmissionQueueReplyInThreadEnabled(msg.ChatType)); err != nil {
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
	mode := a.SubmissionQueueConfiguredInflightMode()
	hasInFlight := sessionctx.HasInFlightSubmission(sess)
	queueLenBefore := len(sess.Queue)
	shouldAttemptStart := !hasInFlight || a.SubmissionQueueInflightAllowsAdditional(mode)
	willWaitInQueue := queueLenBefore > 0 || (hasInFlight && !a.SubmissionQueueInflightAllowsAdditional(mode))
	if willWaitInQueue {
		sess.Status = state.SessionStatusQueued.String()
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
	)
	if err := appState.SaveSession(sess); err != nil {
		return err
	}
	a.SubmissionQueueLogSessionState("submission enqueue session persisted", sessionKey, appState.Session(sessionKey))
	sub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          workspaceID,
		UserID:               msg.UserID,
		ChatID:               msg.ChatID,
		TriggerMessageID:     msg.MessageID,
		SourceMessageIDs:     sourceMessageIDs,
		SourceRootMessageIDs: sourceRootMessageIDs,
		InputText:            skillResolution.InputText,
		Skills:               skillResolution.Skills,
		Attachments:          attachments,
		Status:               state.SubmissionStatusQueued.String(),
		WaitedInQueue:        willWaitInQueue,
	}
	id, err := appState.CreateSubmission(sub)
	if err != nil {
		return err
	}
	if err := appState.QueueSubmission(sessionKey, id); err != nil {
		return err
	}
	sub.ID = id
	if len(stagedImages) > 0 {
		if err := pendingQueue.ClearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
			return err
		}
	}
	if skillResolution.ConsumePending {
		a.SubmissionQueueSkillResolver().ClearSessionPendingSkill(sessionKey)
	}
	slog.Debug("submission queued",
		"submission_id", id,
		"session_key", sessionKey,
		"active_thread_id", sess.ActiveThreadID,
		"active_turn_id", sess.ActiveTurnID,
	)
	a.SubmissionQueueLogSessionState("submission queued session snapshot", sessionKey, appState.Session(sessionKey))
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
	a.SubmissionQueueMarkSubmissionQueuedReactions(sub)
	a.SubmissionQueueSendQueuedNotice(context.Background(), sub)
	return nil
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
	a := s.App
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}
	if !a.SubmissionQueueTryBeginStart(sessionKey) {
		slog.Debug("startNextSubmission coalesced",
			"session_key", sessionKey,
		)
		return nil
	}
	defer func() {
		if a.SubmissionQueueFinishStart(sessionKey) {
			a.SubmissionQueueRunAsync(func() {
				s.StartNextSubmissionAsync(sessionKey, "coalesced")
			})
		}
	}()

	appState := a.SubmissionQueueAppState()
	for {
		sess := appState.Session(sessionKey)
		a.SubmissionQueueLogSessionState("startNextSubmission entry", sessionKey, sess)
		if sess == nil {
			slog.Debug("startNextSubmission skipped", "session_key", sessionKey, "has_session", false)
			return nil
		}
		nextMode := a.SubmissionQueueConfiguredInflightMode()
		if len(sess.Queue) > 0 {
			if nextSub := appState.Submission(sess.Queue[0]); nextSub != nil {
				nextMode = a.SubmissionQueueConfiguredInflightMode()
			}
		}
		if sessionctx.HasInFlightSubmission(sess) && !a.SubmissionQueueInflightAllowsAdditional(nextMode) {
			slog.Debug("startNextSubmission skipped",
				"session_key", sessionKey,
				"has_session", true,
				"active_turn_id", sess.ActiveTurnID,
			)
			return nil
		}
		if runtime := a.SubmissionQueueBackendRuntime(); runtime != nil && runtime.DeferQueuedSubmissionsDuringRecovery() {
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
				updatedSess, updateErr := appState.UpdateSession(sessionKey, func(current *state.Session) {
					if current == nil {
						return
					}
					RefreshPendingStatus(current)
				})
				if updateErr != nil {
					return updateErr
				}
				a.SubmissionQueueLogSessionState("startNextSubmission empty-after-dequeue", sessionKey, updatedSess)
			} else {
				a.SubmissionQueueLogSessionState("startNextSubmission empty-after-dequeue", sessionKey, appState.Session(sessionKey))
			}
			return err
		}
		a.SubmissionQueueLogSessionState("startNextSubmission after dequeue", sessionKey, appState.Session(sessionKey))
		sub := appState.Submission(subID)
		if sub == nil {
			slog.Warn("queued submission missing",
				"session_key", sessionKey,
				"submission_id", subID,
			)
			updatedSess, updateErr := appState.UpdateSession(sessionKey, func(current *state.Session) {
				if current == nil {
					return
				}
				RefreshPendingStatus(current)
			})
			if updateErr != nil {
				return updateErr
			}
			a.SubmissionQueueLogSessionState("startNextSubmission after missing queued item", sessionKey, updatedSess)
			if updatedSess != nil && !sessionctx.HasInFlightSubmission(updatedSess) && len(updatedSess.Queue) > 0 {
				continue
			}
			return nil
		}
		ws := a.SubmissionQueueWorkspace(sub.WorkspaceID)
		if ws == nil {
			slog.Error("workspace resolution failed",
				"submission_id", sub.ID,
				"workspace_id", sub.WorkspaceID,
				"default_workspace_id", a.SubmissionQueueDefaultWorkspaceID(),
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
		return a.SubmissionQueueConversationBackend().StartQueuedSubmission(sessionKey, sess, sub, ws, notifyFailure)
	}
}

// ShouldStartNextSubmissionAsync returns true when a session has queued
// submissions and no in-flight work, meaning it is safe to start the next
// submission asynchronously.
func ShouldStartNextSubmissionAsync(sess *state.Session) bool {
	if sess == nil {
		return false
	}
	return !sessionctx.HasInFlightSubmission(sess) && len(sess.Queue) > 0
}

// RefreshPendingStatus refreshes the session status based on queue state.
func RefreshPendingStatus(sess *state.Session) {
	if sess == nil || sessionctx.HasInFlightSubmission(sess) {
		return
	}
	if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		sess.Status = state.SessionStatusQueued.String()
		return
	}
	sess.Status = state.SessionStatusIdle.String()
}

// HandleSubmissionStartFailure handles a submission start failure.
func (s SubmissionQueueService) HandleSubmissionStartFailure(sessionKey, threadID string, sub *state.Submission, err error, notifyFailure bool) {
	a := s.App
	appState := a.SubmissionQueueAppState()
	dropThreadLineage := false
	if runtime := a.SubmissionQueueBackendRuntime(); runtime != nil {
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
		a.SubmissionQueueRuntimeState().ClearPendingTurnBindingForSubmission(threadID, sub.ID)
	}
	a.SubmissionQueueClearSubmissionProcessingReactions(sub)
	if sub != nil {
		_ = appState.FinalizeSubmission(sub.ID, state.SubmissionStatusFailed.String())
	}
	shouldStartNext := false
	clearedThreadLineage := false
	if sess, saveErr := appState.UpdateSession(sessionKey, func(sess *state.Session) {
		if sess == nil {
			return
		}
		if sub != nil {
			sessionctx.RemoveActiveOperation(sess, sub.ID, "")
		}
		if dropThreadLineage && strings.TrimSpace(threadID) != "" && strings.TrimSpace(sess.ActiveThreadID) == strings.TrimSpace(threadID) {
			sessionctx.ClearThreadContext(sess)
			sessionctx.ClearBackendThread(sess, "codex")
			clearedThreadLineage = true
		}
		if !sessionctx.HasActiveOperations(sess) {
			if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
				sess.Status = state.SessionStatusQueued.String()
			} else {
				sess.Status = state.SessionStatusIdle.String()
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
		shouldStartNext = sess != nil && !sessionctx.HasInFlightSubmission(sess) && len(sess.Queue) > 0
		a.SubmissionQueueAutoRetry().ObserveAutoRetryTerminal(sessionKey, threadID, "failed", sess, sub, "")
	}
	if clearedThreadLineage {
		a.SubmissionQueueLiveThread().ClearSessionLiveThread(sessionKey)
	}
	if notifyFailure && sub != nil {
		willContinue := shouldStartNext
		a.SubmissionQueueSendStartFailureNotice(context.Background(), sub, err, willContinue)
	}
	a.SubmissionQueueRuntimeMaintenance().CleanupSubmissionRuntimeState(sub)
	if shouldStartNext {
		a.SubmissionQueueRunAsync(func() {
			s.StartNextSubmissionAsync(sessionKey, "turnStartFailed")
		})
	}
}

// NotifySubmissionStartFailure sends a failure notification.
func (s SubmissionQueueService) NotifySubmissionStartFailure(ctx context.Context, sub *state.Submission, err error, willContinue bool) {
	a := s.App
	if sub == nil || err == nil {
		return
	}
	body := "任务启动失败: " + strings.TrimSpace(err.Error())
	if willContinue {
		body += "\n\n本条消息已跳过，正在继续处理后续排队消息。"
	} else {
		body += "\n\n本条消息未开始执行，可稍后重试。"
	}
	inThread := a.SubmissionQueueReplyInThreadForSubmission(sub)
	if strings.TrimSpace(sub.TriggerMessageID) != "" {
		if replyErr := a.SubmissionQueueReplyText(ctx, sub.TriggerMessageID, body, inThread); replyErr == nil {
			return
		}
	}
	_ = inThread
	if strings.TrimSpace(sub.ChatID) != "" {
		_ = a.SubmissionQueueReplyText(ctx, sub.ChatID, body, false)
	}
}

// StartNextSubmissionAsync asynchronously starts the next submission if needed.
func (s SubmissionQueueService) StartNextSubmissionAsync(sessionKey, source string) {
	a := s.App
	appState := a.SubmissionQueueAppState()
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	sess := appState.Session(sessionKey)
	if sess == nil || sessionctx.HasInFlightSubmission(sess) || len(sess.Queue) == 0 {
		return
	}
	if err := s.StartNextSubmissionWithFailureNotice(sessionKey, true); err != nil {
		slog.Error("async startNextSubmission failed",
			"session_key", sessionKey,
			"source", source,
			"error", err,
		)
		a.SubmissionQueueLogSessionState("async startNextSubmission failed snapshot", sessionKey, appState.Session(sessionKey))
	}
}

// StartNextCodexSubmissionWithFailureNotice handles Codex-specific submission
// startup: thread creation, turn start, and state binding.
func (s SubmissionQueueService) StartNextCodexSubmissionWithFailureNotice(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	a := s.App
	appState := a.SubmissionQueueAppState()
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	if !sessionctx.CanResumeThreadForSubmission(sess, sub) {
		threadID = ""
		sessionctx.ClearThreadContext(sess)
	}
	if threadID != "" && !a.SubmissionQueueLiveThread().SessionHasLiveThread(sessionKey, threadID) {
		slog.Warn("dropping non-live session thread before submission",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"workspace_id", sub.WorkspaceID,
		)
		threadID = ""
		sessionctx.ClearThreadContext(sess)
	}
	effectiveModel := ""
	effectiveReasoningEffort := ""
	effectiveApprovalPolicy := sessionctx.EffectiveApprovalPolicy(sess, ws)
	effectiveSandboxMode := sessionctx.EffectiveSandboxMode(sess, ws)
	effectiveServiceTier := sessionctx.EffectiveServiceTier(sess)
	if threadID == "" {
		client, err := a.SubmissionQueueRequireCodexClient()
		if err != nil {
			s.HandleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
			a.SubmissionQueueLogSessionState("startNextSubmission thread-client-missing", sessionKey, appState.Session(sessionKey))
			return err
		}
		threadParams := a.SubmissionQueueBuildThreadStartParams(ws, sess, effectiveModel)
		var threadResp codexrpc.ThreadStartResult
		slog.Debug("thread start request",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"cwd", ws.Cwd,
			"model", effectiveModel,
		)
		threadCtx, threadCancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = client.Call(threadCtx, "thread/start", threadParams.Map(), &threadResp)
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
			a.SubmissionQueueLogSessionState("startNextSubmission thread-start-failed", sessionKey, appState.Session(sessionKey))
			return err
		}
		threadID = threadResp.Thread.ID
		slog.Debug("thread started",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"model", effectiveModel,
		)
		sessionctx.SetThreadContext(sess, sub.WorkspaceID, threadID, threadResp.Thread.Name, threadResp.Thread.Preview)
		a.SubmissionQueueLiveThread().MarkSessionThreadLive(sessionKey, threadID)
	}
	if threadID != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
		sessionctx.SetThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	}
	if threadID != "" {
		a.SubmissionQueueLiveThread().MarkSessionThreadLive(sessionKey, threadID)
	}
	sessionctx.UpsertActiveOperation(sess, state.SessionActiveOperation{
		Kind:         sessionctx.OpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
	})
	sess.Status = state.SessionStatusTurnStarting.String()
	sub.ThreadID = threadID
	sub.Status = state.SubmissionStatusRunning.String()
	a.SubmissionQueueRuntimeState().NotePendingTurnBinding(threadID, sessionKey, sub.ID)
	if err := appState.SaveSession(sess); err != nil {
		a.SubmissionQueueRuntimeState().ClearPendingTurnBindingForSubmission(threadID, sub.ID)
		return err
	}
	if err := appState.MarkSubmissionRunning(sub.ID, threadID, ""); err != nil {
		a.SubmissionQueueRuntimeState().ClearPendingTurnBindingForSubmission(threadID, sub.ID)
		return err
	}
	a.SubmissionQueueMarkSubmissionRunningReactions(sub)
	a.SubmissionQueueLogSessionState("startNextSubmission session starting", sessionKey, appState.Session(sessionKey))
	turnCtx, turnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	turnID := ""
	var turnErr error
	if a.SubmissionQueueIsReviewSubmission(sub) {
		turnID, turnErr = a.SubmissionQueueStartSubmissionReview(turnCtx, threadID, sub)
	} else {
		turnID, turnErr = a.SubmissionQueueStartSubmissionTurn(turnCtx, sessionKey, threadID, sub, ws.Cwd, effectiveApprovalPolicy, effectiveSandboxMode, effectiveServiceTier, effectiveModel, effectiveReasoningEffort)
	}
	turnCancel()
	if turnErr != nil {
		if errors.Is(turnErr, context.DeadlineExceeded) {
			slog.Warn("turn start timed out; waiting for delayed notification",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"thread_id", threadID,
				"workspace_id", sub.WorkspaceID,
			)
			a.SubmissionQueueLogSessionState("startNextSubmission awaiting turn-start-notification", sessionKey, appState.Session(sessionKey))
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
		a.SubmissionQueueLogSessionState("startNextSubmission turn-start-failed", sessionKey, appState.Session(sessionKey))
		return turnErr
	}
	slog.Debug("turn started",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	sessionctx.UpsertActiveOperation(sess, state.SessionActiveOperation{
		Kind:         sessionctx.OpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
		TurnID:       turnID,
	})
	sess.Status = state.SessionStatusTurnInProgress.String()
	a.SubmissionQueueRuntimeState().BindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	a.SubmissionQueueRuntimeState().MarkTurnStartedAt(turnID, time.Now())
	a.SubmissionQueueRuntimeState().ClearPendingTurnBindingForSubmission(threadID, sub.ID)
	sub.ThreadID = threadID
	sub.TurnID = turnID
	sub.Status = state.SubmissionStatusRunning.String()
	if err := appState.SaveSession(sess); err != nil {
		return err
	}
	a.SubmissionQueueLogSessionState("startNextSubmission session activated", sessionKey, appState.Session(sessionKey))
	if err := appState.MarkSubmissionRunning(sub.ID, threadID, turnID); err != nil {
		return err
	}
	a.SubmissionQueueReplyContinuation().RecordSubmissionSourceLinks(sub)
	a.SubmissionQueueReplyContinuation().RecordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
	a.SubmissionQueueTurnStream().NoteTurnStarted(sessionKey, sub)
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
