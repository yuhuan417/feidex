// Package turnlifecycle handles turn binding, completion, and notification
// dispatch. Extracted from the app god package.
package turnlifecycle

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/app/apputil"
	"feidex/internal/app/sessionctx"
	"feidex/internal/app/submission"
	appturnstream "feidex/internal/app/turnstream"
	"feidex/internal/state"
)

// prependAttentionMentionMarkdown is a local alias for the apputil helper.
var (
	prependAttentionMentionMarkdown = apputil.PrependAttentionMentionMarkdown
	firstNonEmpty                   = apputil.FirstNonEmpty
)

// ---------------------------------------------------------------------------
// App interface — what the service needs from the host application
// ---------------------------------------------------------------------------

// App defines the interface the turn lifecycle service requires from the host
// application.
type App interface {
	// Provider accessors — return narrow interfaces for each dependency.
	TurnLifecycleAppState() AppStateProvider
	TurnLifecycleRuntimeState() RuntimeStateProvider
	TurnLifecycleReplyContinuation() ReplyContinuationProvider
	TurnLifecycleTurnStream() TurnStreamProvider
	TurnLifecyclePendingQueue() PendingQueueProvider
	TurnLifecycleOutboundCard() OutboundCardProvider
	TurnLifecycleSubmissionDispatch() SubmissionDispatchProvider
	TurnLifecycleAutoRetry() AutoRetryProvider
	TurnLifecycleRuntimeMaintenance() RuntimeMaintenanceProvider

	// Direct app methods.
	MarkSessionThreadLive(sessionKey, threadID string)
	RunAsync(fn func())
	TurnStopAttentionUserID(sub *state.Submission, turnID string) string
	SendEmptyFinalCardWithReuse(ctx context.Context, sub *state.Submission, footerLines []string, reuseMessageID string) string
	SendFinalMessagesWithReuse(ctx context.Context, sub *state.Submission, text string, footerLines []string, reuseMessageID string) []string
	SessionHasActiveWork(sess *state.Session) bool
	SessionShouldStartNextSubmissionAsync(sess *state.Session) bool
	NextQueuedSubmissionSessionKey(sessionKey string) string
	BindStandaloneCompactTurn(threadID, turnID string) bool
	BindGoalContinuationTurn(threadID, turnID string) bool
	FinishStandaloneCompactTurn(threadID, turnID, status string) bool
	FindSubmissionByTurn(threadID, turnID string) (string, *state.Submission)
	ProcessCodexPlanModeExitOnTurnCompleted(sessionKey string, sub *state.Submission, threadID, turnID, status string, flush TurnStreamFlushResult) bool
	LogSessionState(event, sessionKey string, sess *state.Session)
}

// ---------------------------------------------------------------------------
// Narrow provider interfaces
// ---------------------------------------------------------------------------

// AppStateProvider narrows app state access to the methods used by the service.
type AppStateProvider interface {
	Session(key string) *state.Session
	Sessions() []*state.Session
	Submission(id string) *state.Submission
	SaveSession(sess *state.Session) error
	MarkSubmissionRunning(id, threadID, turnID string) error
	FinalizeSubmission(id, status string) error
	UpdateSession(key string, mutate func(*state.Session)) (*state.Session, error)
}

// RuntimeStateProvider narrows runtime state access to the methods used by
// the service for turn binding.
type RuntimeStateProvider interface {
	PendingSubmissionForThread(threadID string) (string, *state.Submission)
	BindTurnSubmission(threadID, turnID, sessionKey, submissionID string)
	MarkTurnStartedAt(turnID string, startedAt time.Time)
	ClearPendingTurnBindingForSubmission(threadID, submissionID string)
	TurnFinalFooterLines(turnID string, completedAt time.Time) []string
}

// ReplyContinuationProvider narrows reply continuation access to the methods
// used by the service.
type ReplyContinuationProvider interface {
	RecordSubmissionSourceLinks(sub *state.Submission)
	RecordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID string)
}

// TurnStreamFlushResult is an alias for the turnstream.FlushResult type.
type TurnStreamFlushResult = appturnstream.FlushResult

// TurnStreamProvider narrows turn stream access to the methods used by the
// service.
type TurnStreamProvider interface {
	NoteTurnStarted(sessionKey string, sub *state.Submission)
	FlushTurnStream(ctx context.Context, threadID, turnID string) TurnStreamFlushResult
}

// PendingQueueProvider narrows pending queue access to the methods used by
// the service.
type PendingQueueProvider interface {
	ClearSubmissionProcessingReactions(sub *state.Submission)
}

// OutboundCardProvider narrows outbound card access to the methods used by
// the service.
type OutboundCardProvider interface {
	ReplaceTurnEventCardWithReuse(ctx context.Context, sub *state.Submission, title, color, body, kind, itemID, reuseMessageID string) string
}

// SubmissionDispatchProvider narrows submission dispatch access to the
// methods used by the service.
type SubmissionDispatchProvider interface {
	StartNextSubmissionAsync(sessionKey, source string)
}

// AutoRetryProvider narrows auto-retry access to the methods used by the
// service.
type AutoRetryProvider interface {
	ObserveAutoRetryTerminal(sessionKey, threadID, status string, updatedSess *state.Session, sub *state.Submission, reuseMessageID string) bool
}

// RuntimeMaintenanceProvider narrows runtime maintenance access to the
// methods used by the service.
type RuntimeMaintenanceProvider interface {
	CleanupSubmissionRuntimeState(sub *state.Submission)
}

// ---------------------------------------------------------------------------
// Service — manages the turn lifecycle
// ---------------------------------------------------------------------------

// Service manages turn binding, completion, and notification dispatch for a
// single app instance.
type Service struct {
	app App
}

// NewService creates a new turn lifecycle service bound to the given app.
func NewService(app App) Service {
	return Service{app: app}
}

// Provider accessors — delegate to App methods for testability.

func (w Service) stateProvider() AppStateProvider    { return w.app.TurnLifecycleAppState() }
func (w Service) runtimeState() RuntimeStateProvider { return w.app.TurnLifecycleRuntimeState() }
func (w Service) replyContinuation() ReplyContinuationProvider {
	return w.app.TurnLifecycleReplyContinuation()
}
func (w Service) turnStream() TurnStreamProvider     { return w.app.TurnLifecycleTurnStream() }
func (w Service) pendingQueue() PendingQueueProvider { return w.app.TurnLifecyclePendingQueue() }
func (w Service) outboundCard() OutboundCardProvider { return w.app.TurnLifecycleOutboundCard() }
func (w Service) submissionDispatch() SubmissionDispatchProvider {
	return w.app.TurnLifecycleSubmissionDispatch()
}
func (w Service) autoRetry() AutoRetryProvider { return w.app.TurnLifecycleAutoRetry() }
func (w Service) runtimeMaintenance() RuntimeMaintenanceProvider {
	return w.app.TurnLifecycleRuntimeMaintenance()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func isReviewSubmission(sub *state.Submission) bool {
	return sub != nil && strings.TrimSpace(sub.Kind) == "review"
}

func sessionHasActiveOperations(sess *state.Session) bool {
	return sessionctx.HasActiveOperations(sess)
}

func sessionHasActiveWork(sess *state.Session) bool {
	if sess == nil {
		return false
	}
	if sessionHasActiveOperations(sess) {
		return true
	}
	switch state.NormalizeSessionStatus(sess.Status) {
	case state.SessionStatusTurnStarting:
		return true
	default:
		return false
	}
}

func sessionShouldStartNextSubmissionAsync(sess *state.Session) bool {
	return submission.ShouldStartNextSubmissionAsync(sess)
}

// TurnCompletionTerminalText returns the terminal text to display when a turn
// completes. Returns "" for successful completions. This is a pure function.
func TurnCompletionTerminalText(status, lastError string) string {
	return submission.CompletionTerminalText(status, lastError)
}

// ---------------------------------------------------------------------------
// Exported service methods
// ---------------------------------------------------------------------------

// BindPendingSubmissionTurn attempts to bind a pending submission to the
// given turn. Returns true if binding succeeded.
func (w Service) BindPendingSubmissionTurn(threadID, turnID string, allowReview bool) bool {
	st := w.stateProvider()
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return false
	}
	sessionKey, sub := w.runtimeState().PendingSubmissionForThread(threadID)
	if sub == nil {
		return false
	}
	if isReviewSubmission(sub) && !allowReview {
		return false
	}
	w.runtimeState().BindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	w.runtimeState().MarkTurnStartedAt(turnID, time.Now())
	w.runtimeState().ClearPendingTurnBindingForSubmission(threadID, sub.ID)

	sess := st.Session(sessionKey)
	if sess == nil {
		return false
	}
	sessionctx.UpsertActiveOperation(sess, state.SessionActiveOperation{
		Kind:         sessionctx.OpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
		TurnID:       turnID,
	})
	sess.Status = state.SessionStatusTurnInProgress.String()
	sessionctx.SetThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	if err := st.SaveSession(sess); err != nil {
		return false
	}
	_ = st.MarkSubmissionRunning(sub.ID, threadID, turnID)
	sub.ThreadID = threadID
	sub.TurnID = turnID
	sub.Status = state.SubmissionStatusRunning.String()
	w.replyContinuation().RecordSubmissionSourceLinks(sub)
	w.replyContinuation().RecordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
	w.turnStream().NoteTurnStarted(sessionKey, sub)
	w.app.MarkSessionThreadLive(sessionKey, threadID)
	return true
}

// OnTurnStartedNotification handles a turn-started notification by
// attempting to bind a pending submission, falling back to standalone
// compact turn binding.
func (w Service) OnTurnStartedNotification(threadID, turnID string) {
	st := w.stateProvider()
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return
	}

	if w.BindPendingSubmissionTurn(threadID, turnID, false) {
		return
	}
	if w.app.BindStandaloneCompactTurn(threadID, turnID) {
		return
	}
	if w.app.BindGoalContinuationTurn(threadID, turnID) {
		return
	}

	sessionKey := ""
	submissionID := ""
	for _, candidate := range st.Sessions() {
		if candidate == nil {
			continue
		}
		if sessionctx.FindActiveOperationByTurn(candidate, turnID) != nil {
			return
		}
		op := sessionctx.FindPendingSubmissionOperationByThread(candidate, threadID)
		if op == nil {
			continue
		}
		if strings.TrimSpace(op.SubmissionID) == "" {
			continue
		}
		sessionKey = candidate.Key
		submissionID = strings.TrimSpace(op.SubmissionID)
		break
	}
	if sessionKey == "" {
		return
	}
	sess := st.Session(sessionKey)
	if sess == nil {
		slog.Warn("turn started notification missing session",
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
		)
		return
	}
	sub := st.Submission(submissionID)
	if sub == nil {
		slog.Warn("turn started notification missing submission",
			"session_key", sessionKey,
			"submission_id", submissionID,
			"thread_id", threadID,
			"turn_id", turnID,
		)
		return
	}
	sessionctx.UpsertActiveOperation(sess, state.SessionActiveOperation{
		Kind:         sessionctx.OpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
		TurnID:       turnID,
	})
	sess.Status = state.SessionStatusTurnInProgress.String()
	sessionctx.SetThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	if err := st.SaveSession(sess); err != nil {
		slog.Error("turn started notification session bind failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
			"error", err,
		)
		return
	}
	_ = st.MarkSubmissionRunning(sub.ID, threadID, turnID)
	sub.ThreadID = threadID
	sub.TurnID = turnID
	sub.Status = state.SubmissionStatusRunning.String()
	w.runtimeState().BindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	w.runtimeState().MarkTurnStartedAt(turnID, time.Now())
	w.runtimeState().ClearPendingTurnBindingForSubmission(threadID, sub.ID)
	w.replyContinuation().RecordSubmissionSourceLinks(sub)
	w.replyContinuation().RecordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
	w.turnStream().NoteTurnStarted(sessionKey, sub)
	w.app.MarkSessionThreadLive(sessionKey, threadID)
	slog.Debug("turn started notification rebound pending submission",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	w.app.LogSessionState("turn started notification session snapshot", sessionKey, st.Session(sessionKey))
}

// BindPendingSubmissionForTurnCompletion attempts to bind a pending
// submission to a turn that is completing (no prior turn-start notification).
// Returns the session key and submission if binding succeeded.
func (w Service) BindPendingSubmissionForTurnCompletion(threadID, turnID string) (string, *state.Submission) {
	st := w.stateProvider()
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return "", nil
	}

	sessionKey, sub := w.runtimeState().PendingSubmissionForThread(threadID)
	if sub == nil || strings.TrimSpace(sub.TurnID) != "" || sub.Finalized {
		return "", nil
	}
	sess := st.Session(sessionKey)
	if sess == nil {
		return "", nil
	}
	op := sessionctx.FindPendingSubmissionOperationByThread(sess, threadID)
	if op == nil {
		return "", nil
	}
	if strings.TrimSpace(op.SubmissionID) != sub.ID {
		return "", nil
	}
	if strings.TrimSpace(op.TurnID) != "" {
		return "", nil
	}

	w.runtimeState().BindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	w.runtimeState().MarkTurnStartedAt(turnID, time.Now())
	w.runtimeState().ClearPendingTurnBindingForSubmission(threadID, sub.ID)

	sessionctx.UpsertActiveOperation(sess, state.SessionActiveOperation{
		Kind:         sessionctx.OpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
		TurnID:       turnID,
	})
	sess.Status = state.SessionStatusTurnInProgress.String()
	sessionctx.SetThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	if err := st.SaveSession(sess); err != nil {
		slog.Error("turn completed fallback session bind failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
			"error", err,
		)
		return "", nil
	}
	if err := st.MarkSubmissionRunning(sub.ID, threadID, turnID); err != nil {
		slog.Error("turn completed fallback submission bind failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
			"error", err,
		)
		return "", nil
	}
	sub = st.Submission(sub.ID)
	if sub == nil {
		return "", nil
	}
	w.replyContinuation().RecordSubmissionSourceLinks(sub)
	w.replyContinuation().RecordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
	w.turnStream().NoteTurnStarted(sessionKey, sub)
	w.app.MarkSessionThreadLive(sessionKey, threadID)
	slog.Debug("turn completed rebound pending submission without prior turn start notification",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	return sessionKey, sub
}

// FinishTurn finalizes a turn, cleans up session state, delivers terminal
// cards, and optionally schedules the next submission.
func (w Service) FinishTurn(threadID, turnID, status string) {
	st := w.stateProvider()
	sessionKey, sub := w.app.FindSubmissionByTurn(threadID, turnID)
	slog.Debug("finishTurn entry",
		"thread_id", threadID,
		"turn_id", turnID,
		"status", status,
		"session_key", sessionKey,
		"found_submission", sub != nil,
	)
	if sub == nil {
		sessionKey, sub = w.BindPendingSubmissionForTurnCompletion(threadID, turnID)
	}
	if sub == nil {
		if w.app.FinishStandaloneCompactTurn(threadID, turnID, status) {
			return
		}
		slog.Warn("finishTurn missing submission",
			"thread_id", threadID,
			"turn_id", turnID,
			"status", status,
		)
		return
	}
	if sub.Finalized {
		slog.Debug("finishTurn ignored finalized submission",
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
		)
		return
	}

	flush := w.turnStream().FlushTurnStream(context.Background(), threadID, turnID)

	switch status {
	case "completed":
		_ = st.FinalizeSubmission(sub.ID, "completed")
	case "interrupted":
		_ = st.FinalizeSubmission(sub.ID, "interrupted")
	default:
		_ = st.FinalizeSubmission(sub.ID, "failed")
	}
	terminalText := ""
	attentionUserID := ""
	reuseMessageID := strings.TrimSpace(flush.WorkingMessageID)
	sub = st.Submission(sub.ID)
	if sub != nil {
		w.pendingQueue().ClearSubmissionProcessingReactions(sub)
		slog.Debug("submission finalized",
			"submission_id", sub.ID,
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
			"status", sub.Status,
		)
		terminalText = TurnCompletionTerminalText(sub.Status, flush.LastError)
		attentionUserID = w.app.TurnStopAttentionUserID(sub, turnID)
	}
	if sess := st.Session(sessionKey); sess != nil {
		w.app.LogSessionState("finishTurn before session cleanup", sessionKey, sess)
	}
	updatedSess, _ := st.UpdateSession(sessionKey, func(sess *state.Session) {
		if sess == nil {
			return
		}
		sessionctx.RemoveActiveOperation(sess, sub.ID, turnID)
		switch {
		case sessionHasActiveOperations(sess):
			sess.Status = state.SessionStatusTurnStarting.String()
			for _, op := range sess.ActiveOperations {
				if strings.TrimSpace(op.TurnID) != "" {
					sess.Status = state.SessionStatusTurnInProgress.String()
					break
				}
			}
		case len(sess.Queue) > 0 || len(sess.StagedImages) > 0:
			sess.Status = state.SessionStatusQueued.String()
		default:
			sess.Status = state.SessionStatusIdle.String()
		}
	})
	suppressTerminalCard := false
	if updatedSess != nil {
		slog.Debug("finishTurn session state after cleanup",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"status", updatedSess.Status,
			"active_operations_count", len(updatedSess.ActiveOperations),
			"queue_len", len(updatedSess.Queue),
			"has_in_flight", sessionHasActiveWork(updatedSess),
		)
		w.app.LogSessionState("finishTurn after session cleanup", sessionKey, updatedSess)
		suppressTerminalCard = w.autoRetry().ObserveAutoRetryTerminal(sessionKey, threadID, sub.Status, updatedSess, sub, reuseMessageID)
	}
	if terminalText != "" && !suppressTerminalCard {
		w.outboundCard().ReplaceTurnEventCardWithReuse(
			context.Background(),
			sub,
			"任务状态",
			"grey",
			prependAttentionMentionMarkdown(terminalText, attentionUserID),
			"turn_terminal",
			"",
			reuseMessageID,
		)
	}
	// Finalize steer submissions that were part of the same thread.  This
	// must happen synchronously BEFORE the async StartNextSubmissionAsync
	// below, otherwise the newly started submission (which shares the same
	// thread) would be picked up by a post-hoc steer scan and incorrectly
	// finalized as "completed", clearing the session to idle.
	for _, op := range updatedSess.ActiveOperations {
		if strings.TrimSpace(op.ThreadID) != threadID {
			continue
		}
		opTurnID := strings.TrimSpace(op.TurnID)
		if opTurnID != "" && opTurnID == turnID {
			continue
		}
		steerSub := st.Submission(strings.TrimSpace(op.SubmissionID))
		if steerSub == nil || steerSub.Finalized {
			continue
		}
		switch status {
		case "completed":
			_ = st.FinalizeSubmission(steerSub.ID, "completed")
		case "interrupted":
			_ = st.FinalizeSubmission(steerSub.ID, "interrupted")
		default:
			_ = st.FinalizeSubmission(steerSub.ID, "failed")
		}
		w.pendingQueue().ClearSubmissionProcessingReactions(steerSub)
		if _, err := st.UpdateSession(sessionKey, func(s *state.Session) {
			if s == nil {
				return
			}
			sessionctx.RemoveActiveOperation(s, steerSub.ID, opTurnID)
			submission.RefreshPendingStatus(s)
		}); err != nil {
			slog.Error("finishTurn steer cleanup session update failed",
				"session_key", sessionKey,
				"steer_submission_id", steerSub.ID,
				"error", err,
			)
		}
	}
	// Re-read session after steer cleanup to get accurate status for
	// ShouldStartNextSubmissionAsync.
	if refreshedSess := st.Session(sessionKey); refreshedSess != nil {
		updatedSess = refreshedSess
	}
	planExitPromptSent := w.app.ProcessCodexPlanModeExitOnTurnCompleted(sessionKey, sub, threadID, turnID, status, flush)
	if sub != nil && state.NormalizeSubmissionStatus(sub.Status) == state.SubmissionStatusCompleted && !flush.SawFinal && !planExitPromptSent {
		if flush.ShouldUsePlanExitPrompt && strings.TrimSpace(flush.PlanMarkdown) != "" {
			w.outboundCard().ReplaceTurnEventCardWithReuse(
				context.Background(),
				sub,
				"计划更新",
				"blue",
				"计划:\n"+strings.TrimSpace(flush.PlanMarkdown),
				"turn_plan",
				"",
				firstNonEmpty(strings.TrimSpace(flush.PlanMessageID), reuseMessageID),
			)
		} else if strings.TrimSpace(flush.FinalText) != "" {
			w.app.SendFinalMessagesWithReuse(
				context.Background(), sub,
				strings.TrimSpace(flush.FinalText),
				w.runtimeState().TurnFinalFooterLines(turnID, time.Now()),
				strings.TrimSpace(flush.FinalReuseMessageID),
			)
		} else {
			w.app.SendEmptyFinalCardWithReuse(
				context.Background(), sub,
				w.runtimeState().TurnFinalFooterLines(turnID, time.Now()),
				reuseMessageID,
			)
		}
	}
	nextSessionKey := ""
	if updatedSess != nil {
		nextSessionKey = strings.TrimSpace(w.app.NextQueuedSubmissionSessionKey(sessionKey))
	}
	if nextSessionKey != "" {
		slog.Debug("finishTurn scheduling next submission asynchronously",
			"session_key", nextSessionKey,
			"source_session_key", sessionKey,
			"thread_id", updatedSess.ActiveThreadID,
		)
		w.app.RunAsync(func() {
			w.submissionDispatch().StartNextSubmissionAsync(nextSessionKey, "finishTurn")
		})
	}
	w.runtimeMaintenance().CleanupSubmissionRuntimeState(sub)
}
