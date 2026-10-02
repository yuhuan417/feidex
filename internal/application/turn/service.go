// Package turnlifecycle handles turn binding, completion, and notification
// dispatch. Extracted from the app god package.
package turn

import (
	"context"
	"feidex/internal/application/presentation"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	domainturn "feidex/internal/domain/turn"
	"feidex/internal/textutil"
	"log/slog"
	"strings"
	"time"
)

func recordLegacySessionRootTurnBinding(reply ReplyContinuationProvider, sess *conversation.Session, sub *domainsubmission.Submission, sessionKey, threadID, turnID string) {
	if reply == nil || sess == nil || sub.HasSourceRootMessages() {
		return
	}
	reply.RecordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
}

// ---------------------------------------------------------------------------
// Narrow provider interfaces
// ---------------------------------------------------------------------------

// AppStateProvider narrows app state access to the methods used by the service.
type AppStateProvider interface {
	Session(key string) *conversation.Session
	Sessions() []*conversation.Session
	Submission(id string) *domainsubmission.Submission
	SaveSession(sess *conversation.Session) error
	MarkSubmissionRunning(id, threadID, turnID string) error
	FinalizeSubmission(id, status string) error
	UpdateSession(key string, mutate func(*conversation.Session)) (*conversation.Session, error)
}

// RuntimeStateProvider narrows runtime state access to the methods used by
// the service for turn binding.
type RuntimeStateProvider interface {
	PendingSubmissionForThread(threadID string) (string, *domainsubmission.Submission)
	BindTurnSubmission(threadID, turnID, sessionKey, submissionID string)
	MarkTurnStartedAt(turnID string, startedAt time.Time)
	ClearPendingTurnBindingForSubmission(threadID, submissionID string)
	TurnFinalFooterLines(turnID string, completedAt time.Time) []string
}

// ReplyContinuationProvider narrows reply continuation access to the methods
// used by the service.
type ReplyContinuationProvider interface {
	RecordSubmissionSourceLinks(sub *domainsubmission.Submission)
	RecordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID string)
}

// TurnStreamFlushResult is the semantic result consumed by turn use cases.
type TurnStreamFlushResult = StreamSummary

// TurnStreamProvider narrows turn stream access to the methods used by the
// service.
type TurnStreamProvider interface {
	NoteTurnStarted(sessionKey string, sub *domainsubmission.Submission)
	FlushTurnStream(ctx context.Context, threadID, turnID string) TurnStreamFlushResult
}

// PendingQueueProvider narrows pending queue access to the methods used by
// the service.
type PendingQueueProvider interface {
	ClearSubmissionProcessingReactions(sub *domainsubmission.Submission)
}

// OutboundCardProvider narrows outbound card access to the methods used by
// the service.
type OutboundCardProvider interface {
	ReplaceTurnEventCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, title, color, body, kind, itemID, reuseMessageID string) string
	SendTerminalCard(ctx context.Context, sub *domainsubmission.Submission, text, attentionUserID, reuseMessageID string)
}

// SubmissionDispatchProvider narrows submission dispatch access to the
// methods used by the service.
type SubmissionDispatchProvider interface {
	StartNextSubmissionAsync(sessionKey, source string)
}

// AutoRetryProvider narrows auto-retry access to the methods used by the
// service.
type AutoRetryProvider interface {
	ObserveAutoRetryTerminal(sessionKey, threadID, status string, updatedSess *conversation.Session, sub *domainsubmission.Submission, reuseMessageID, lastError string) bool
}

// RuntimeMaintenanceProvider narrows runtime maintenance access to the
// methods used by the service.
type RuntimeMaintenanceProvider interface {
	CleanupSubmissionRuntimeState(sub *domainsubmission.Submission)
}

// Dependencies are consumer-owned ports. They are fixed at composition time;
// no service can ask a host App to locate another service for it.
type Dependencies struct {
	State         AppStateProvider
	Bindings      RuntimeStateProvider
	Replies       ReplyContinuationProvider
	Streams       TurnStreamProvider
	Reactions     PendingQueueProvider
	Cards         OutboundCardProvider
	Queue         QueueProvider
	Retry         AutoRetryProvider
	Cleanup       RuntimeMaintenanceProvider
	Runtime       RuntimeProvider
	Continuations ContinuationProvider
	Delivery      FinalDeliveryProvider
	Diagnostics   DiagnosticsProvider
}

type QueueProvider interface {
	FindSubmissionByTurn(threadID, turnID string) (string, *domainsubmission.Submission)
	NextQueuedSessionKey(sessionKey string) string
	StartNextSubmissionAsync(sessionKey, source string)
}

type RuntimeProvider interface {
	Context() context.Context
	RunAsync(fn func())
	MarkSessionThreadLive(sessionKey, threadID string)
}

type ContinuationProvider interface {
	BindStandaloneCompactTurn(threadID, turnID string) bool
	BindGoalContinuationTurn(threadID, turnID string) bool
	FinishStandaloneCompactTurn(threadID, turnID, status string) bool
	ProcessCodexPlanModeExitOnTurnCompleted(sessionKey string, sub *domainsubmission.Submission, threadID, turnID, status string, flush StreamSummary) bool
}

type FinalDeliveryProvider interface {
	TurnStopAttentionUserID(sub *domainsubmission.Submission, turnID string) string
	SendEmptyFinalCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, footerLines []string, reuseMessageID string) string
	SendFinalMessagesWithReuse(ctx context.Context, sub *domainsubmission.Submission, text string, footerLines []string, reuseMessageID string) []string
}

type DiagnosticsProvider interface {
	LogSessionState(event, sessionKey string, sess *conversation.Session)
}

type Service struct{ deps Dependencies }

func NewService(deps Dependencies) Service { return Service{deps: deps} }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sessionHasActiveWork(sess *conversation.Session) bool {
	return sess != nil && (conversation.HasActiveOperations(sess) || conversation.NormalizeSessionStatus(sess.Status) == conversation.SessionStatusTurnStarting)
}

// TurnCompletionTerminalText returns the terminal text to display when a turn
// completes. Returns "" for successful completions. This is a pure function.
func TurnCompletionTerminalText(status, lastError string) string {
	return presentation.CompletionTerminalText(status, lastError)
}

// ---------------------------------------------------------------------------
// Exported service methods
// ---------------------------------------------------------------------------

// BindPendingSubmissionTurn attempts to bind a pending submission to the
// given turn. Returns true if binding succeeded.
func (w Service) BindPendingSubmissionTurn(threadID, turnID string, allowReview bool) bool {
	st := w.deps.State
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return false
	}
	sessionKey, sub := w.deps.Bindings.PendingSubmissionForThread(threadID)
	if !domainturn.CanBindPending(threadID, turnID, sub, allowReview) {
		return false
	}
	w.deps.Bindings.BindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	w.deps.Bindings.MarkTurnStartedAt(turnID, time.Now())
	w.deps.Bindings.ClearPendingTurnBindingForSubmission(threadID, sub.ID)

	sess := st.Session(sessionKey)
	if sess == nil {
		return false
	}
	domainturn.BindSession(sess, sub, threadID, turnID)
	if err := st.SaveSession(sess); err != nil {
		return false
	}
	_ = st.MarkSubmissionRunning(sub.ID, threadID, turnID)
	sub.MarkRunning(threadID, turnID)
	w.deps.Replies.RecordSubmissionSourceLinks(sub)
	recordLegacySessionRootTurnBinding(w.deps.Replies, sess, sub, sessionKey, threadID, turnID)
	w.deps.Streams.NoteTurnStarted(sessionKey, sub)
	w.deps.Runtime.MarkSessionThreadLive(sessionKey, threadID)
	return true
}

// OnTurnStartedNotification handles a turn-started notification by
// attempting to bind a pending submission, falling back to standalone
// compact turn binding.
func (w Service) OnTurnStartedNotification(threadID, turnID string) {
	st := w.deps.State
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return
	}

	if w.BindPendingSubmissionTurn(threadID, turnID, false) {
		return
	}
	if w.deps.Continuations.BindStandaloneCompactTurn(threadID, turnID) {
		return
	}
	if w.deps.Continuations.BindGoalContinuationTurn(threadID, turnID) {
		return
	}

	sessionKey := ""
	submissionID := ""
	for _, candidate := range st.Sessions() {
		if candidate == nil {
			continue
		}
		if conversation.FindActiveOperationByTurn(candidate, turnID) != nil {
			return
		}
		op := conversation.FindPendingSubmissionOperationByThread(candidate, threadID)
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
	domainturn.BindSession(sess, sub, threadID, turnID)
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
	sub.MarkRunning(threadID, turnID)
	w.deps.Bindings.BindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	w.deps.Bindings.MarkTurnStartedAt(turnID, time.Now())
	w.deps.Bindings.ClearPendingTurnBindingForSubmission(threadID, sub.ID)
	w.deps.Replies.RecordSubmissionSourceLinks(sub)
	recordLegacySessionRootTurnBinding(w.deps.Replies, sess, sub, sessionKey, threadID, turnID)
	w.deps.Streams.NoteTurnStarted(sessionKey, sub)
	w.deps.Runtime.MarkSessionThreadLive(sessionKey, threadID)
	slog.Debug("turn started notification rebound pending submission",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	w.deps.Diagnostics.LogSessionState("turn started notification session snapshot", sessionKey, st.Session(sessionKey))
}

// BindPendingSubmissionForTurnCompletion attempts to bind a pending
// submission to a turn that is completing (no prior turn-start notification).
// Returns the session key and submission if binding succeeded.
func (w Service) BindPendingSubmissionForTurnCompletion(threadID, turnID string) (string, *domainsubmission.Submission) {
	st := w.deps.State
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return "", nil
	}

	sessionKey, sub := w.deps.Bindings.PendingSubmissionForThread(threadID)
	if sub == nil || strings.TrimSpace(sub.TurnID) != "" || sub.Finalized {
		return "", nil
	}
	sess := st.Session(sessionKey)
	if sess == nil {
		return "", nil
	}
	if !domainturn.CanBindCompletion(sess, sub, threadID, turnID) {
		return "", nil
	}

	w.deps.Bindings.BindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	w.deps.Bindings.MarkTurnStartedAt(turnID, time.Now())
	w.deps.Bindings.ClearPendingTurnBindingForSubmission(threadID, sub.ID)

	domainturn.BindSession(sess, sub, threadID, turnID)
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
	w.deps.Replies.RecordSubmissionSourceLinks(sub)
	recordLegacySessionRootTurnBinding(w.deps.Replies, sess, sub, sessionKey, threadID, turnID)
	w.deps.Streams.NoteTurnStarted(sessionKey, sub)
	w.deps.Runtime.MarkSessionThreadLive(sessionKey, threadID)
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
	st := w.deps.State
	sessionKey, sub := w.deps.Queue.FindSubmissionByTurn(threadID, turnID)
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
		if w.deps.Continuations.FinishStandaloneCompactTurn(threadID, turnID, status) {
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

	flush := w.deps.Streams.FlushTurnStream(w.deps.Runtime.Context(), threadID, turnID)

	_ = st.FinalizeSubmission(sub.ID, domainturn.CompletionStatus(status).String())
	terminalText := ""
	attentionUserID := ""
	reuseMessageID := strings.TrimSpace(flush.WorkingMessageID)
	sub = st.Submission(sub.ID)
	if sub != nil {
		w.deps.Reactions.ClearSubmissionProcessingReactions(sub)
		slog.Debug("submission finalized",
			"submission_id", sub.ID,
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
			"status", sub.Status,
		)
		terminalText = TurnCompletionTerminalText(sub.Status, flush.LastError)
		attentionUserID = w.deps.Delivery.TurnStopAttentionUserID(sub, turnID)
	}
	if sess := st.Session(sessionKey); sess != nil {
		w.deps.Diagnostics.LogSessionState("finishTurn before session cleanup", sessionKey, sess)
	}
	updatedSess, _ := st.UpdateSession(sessionKey, func(sess *conversation.Session) {
		if sess == nil {
			return
		}
		conversation.RemoveActiveOperation(sess, sub.ID, turnID)
		conversation.RefreshActiveStatus(sess)
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
		w.deps.Diagnostics.LogSessionState("finishTurn after session cleanup", sessionKey, updatedSess)
		suppressTerminalCard = w.deps.Retry.ObserveAutoRetryTerminal(sessionKey, threadID, sub.Status, updatedSess, sub, reuseMessageID, flush.LastError)
	}
	if terminalText != "" && !suppressTerminalCard {
		w.deps.Cards.SendTerminalCard(w.deps.Runtime.Context(), sub, terminalText, attentionUserID, reuseMessageID)
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
		_ = st.FinalizeSubmission(steerSub.ID, domainturn.CompletionStatus(status).String())
		w.deps.Reactions.ClearSubmissionProcessingReactions(steerSub)
		if _, err := st.UpdateSession(sessionKey, func(s *conversation.Session) {
			if s == nil {
				return
			}
			conversation.RemoveActiveOperation(s, steerSub.ID, opTurnID)
			conversation.RefreshPendingStatus(s)
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
	planExitPromptSent := w.deps.Continuations.ProcessCodexPlanModeExitOnTurnCompleted(sessionKey, sub, threadID, turnID, status, flush)
	if sub != nil && domainsubmission.NormalizeSubmissionStatus(sub.Status) == domainsubmission.SubmissionStatusCompleted && !flush.SawFinal && !planExitPromptSent {
		if flush.ShouldUsePlanExitPrompt && strings.TrimSpace(flush.PlanMarkdown) != "" {
			w.deps.Cards.ReplaceTurnEventCardWithReuse(
				w.deps.Runtime.Context(),
				sub,
				"计划更新",
				"blue",
				"计划:\n"+strings.TrimSpace(flush.PlanMarkdown),
				"turn_plan",
				"",
				textutil.FirstNonEmpty(strings.TrimSpace(flush.PlanMessageID), reuseMessageID),
			)
		} else if strings.TrimSpace(flush.FinalText) != "" {
			w.deps.Delivery.SendFinalMessagesWithReuse(
				w.deps.Runtime.Context(), sub,
				strings.TrimSpace(flush.FinalText),
				w.deps.Bindings.TurnFinalFooterLines(turnID, time.Now()),
				strings.TrimSpace(flush.FinalReuseMessageID),
			)
		} else {
			w.deps.Delivery.SendEmptyFinalCardWithReuse(
				w.deps.Runtime.Context(), sub,
				w.deps.Bindings.TurnFinalFooterLines(turnID, time.Now()),
				reuseMessageID,
			)
		}
	}
	nextSessionKey := ""
	if updatedSess != nil {
		nextSessionKey = strings.TrimSpace(w.deps.Queue.NextQueuedSessionKey(sessionKey))
	}
	if nextSessionKey != "" {
		slog.Debug("finishTurn scheduling next submission asynchronously",
			"session_key", nextSessionKey,
			"source_session_key", sessionKey,
			"thread_id", updatedSess.ActiveThreadID,
		)
		w.deps.Runtime.RunAsync(func() {
			w.deps.Queue.StartNextSubmissionAsync(nextSessionKey, "finishTurn")
		})
	}
	w.deps.Cleanup.CleanupSubmissionRuntimeState(sub)
}
