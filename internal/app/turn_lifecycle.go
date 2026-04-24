package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/state"
)

func (w *lifecycleCoordinator) bindPendingSubmissionTurn(threadID, turnID string, allowReview bool) bool {
	a := w.app
	appState := appState(a)
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return false
	}
	sessionKey, sub := newRuntimeStateService(a).pendingSubmissionForThread(threadID)
	if sub == nil {
		return false
	}
	if isReviewSubmission(sub) && !allowReview {
		return false
	}
	newRuntimeStateService(a).bindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	newRuntimeStateService(a).markTurnStartedAt(turnID, time.Now())
	newRuntimeStateService(a).clearPendingTurnBindingForSubmission(threadID, sub.ID)

	sess := appState.session(sessionKey)
	if sess == nil {
		return false
	}
	sessionUpsertActiveOperation(sess, state.SessionActiveOperation{
		Kind:         sessionOpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
		TurnID:       turnID,
	})
	sess.Status = "turn_in_progress"
	setSessionThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	if err := appState.saveSession(sess); err != nil {
		return false
	}
	_ = appState.markSubmissionRunning(sub.ID, threadID, turnID)
	sub.ThreadID = threadID
	sub.TurnID = turnID
	sub.Status = "running"
	newReplyContinuationService(a).recordSubmissionSourceLinks(sub)
	newReplyContinuationService(a).recordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
	newTurnStreamService(a).noteTurnStarted(sessionKey, sub)
	a.markSessionThreadLive(sessionKey, threadID)
	return true
}

func (w *lifecycleCoordinator) onTurnStartedNotification(threadID, turnID string) {
	a := w.app
	appState := appState(a)
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return
	}

	if w.bindPendingSubmissionTurn(threadID, turnID, false) {
		return
	}
	if a.bindStandaloneCompactTurn(threadID, turnID) {
		return
	}

	sessionKey := ""
	submissionID := ""
	for _, candidate := range appState.sessions() {
		if candidate == nil {
			continue
		}
		if sessionFindActiveOperationByTurn(candidate, turnID) != nil {
			return
		}
		op := sessionFindPendingSubmissionOperationByThread(candidate, threadID)
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
	sess := appState.session(sessionKey)
	if sess == nil {
		slog.Warn("turn started notification missing session",
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
		)
		return
	}
	sub := appState.submission(submissionID)
	if sub == nil {
		slog.Warn("turn started notification missing submission",
			"session_key", sessionKey,
			"submission_id", submissionID,
			"thread_id", threadID,
			"turn_id", turnID,
		)
		return
	}
	sessionUpsertActiveOperation(sess, state.SessionActiveOperation{
		Kind:         sessionOpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
		TurnID:       turnID,
	})
	sess.Status = "turn_in_progress"
	setSessionThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	if err := appState.saveSession(sess); err != nil {
		slog.Error("turn started notification session bind failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
			"error", err,
		)
		return
	}
	_ = appState.markSubmissionRunning(sub.ID, threadID, turnID)
	sub.ThreadID = threadID
	sub.TurnID = turnID
	sub.Status = "running"
	newRuntimeStateService(a).bindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	newRuntimeStateService(a).markTurnStartedAt(turnID, time.Now())
	newRuntimeStateService(a).clearPendingTurnBindingForSubmission(threadID, sub.ID)
	newReplyContinuationService(a).recordSubmissionSourceLinks(sub)
	newReplyContinuationService(a).recordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
	newTurnStreamService(a).noteTurnStarted(sessionKey, sub)
	a.markSessionThreadLive(sessionKey, threadID)
	slog.Debug("turn started notification rebound pending submission",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	logSessionState("turn started notification session snapshot", sessionKey, appState.session(sessionKey))
}

func (w *lifecycleCoordinator) bindPendingSubmissionForTurnCompletion(threadID, turnID string) (string, *state.Submission) {
	a := w.app
	appState := appState(a)
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return "", nil
	}

	sessionKey, sub := newRuntimeStateService(a).pendingSubmissionForThread(threadID)
	if sub == nil || strings.TrimSpace(sub.TurnID) != "" || sub.Finalized {
		return "", nil
	}
	sess := appState.session(sessionKey)
	if sess == nil {
		return "", nil
	}
	op := sessionFindPendingSubmissionOperationByThread(sess, threadID)
	if op == nil {
		return "", nil
	}
	if strings.TrimSpace(op.SubmissionID) != sub.ID {
		return "", nil
	}
	if strings.TrimSpace(op.TurnID) != "" {
		return "", nil
	}

	newRuntimeStateService(a).bindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	newRuntimeStateService(a).markTurnStartedAt(turnID, time.Now())
	newRuntimeStateService(a).clearPendingTurnBindingForSubmission(threadID, sub.ID)

	sessionUpsertActiveOperation(sess, state.SessionActiveOperation{
		Kind:         sessionOpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
		TurnID:       turnID,
	})
	sess.Status = "turn_in_progress"
	setSessionThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	if err := appState.saveSession(sess); err != nil {
		slog.Error("turn completed fallback session bind failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
			"error", err,
		)
		return "", nil
	}
	if err := appState.markSubmissionRunning(sub.ID, threadID, turnID); err != nil {
		slog.Error("turn completed fallback submission bind failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
			"error", err,
		)
		return "", nil
	}
	sub = appState.submission(sub.ID)
	if sub == nil {
		return "", nil
	}
	newReplyContinuationService(a).recordSubmissionSourceLinks(sub)
	newReplyContinuationService(a).recordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
	newTurnStreamService(a).noteTurnStarted(sessionKey, sub)
	a.markSessionThreadLive(sessionKey, threadID)
	slog.Debug("turn completed rebound pending submission without prior turn start notification",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	return sessionKey, sub
}

func (w *lifecycleCoordinator) finishTurn(threadID, turnID, status string) {
	a := w.app
	appState := appState(a)
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		sessionKey, sub = w.bindPendingSubmissionForTurnCompletion(threadID, turnID)
	}
	if sub == nil {
		if a.finishStandaloneCompactTurn(threadID, turnID, status) {
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

	flush := newTurnStreamService(a).flushTurnStream(context.Background(), threadID, turnID)

	switch status {
	case "completed":
		_ = appState.finalizeSubmission(sub.ID, "completed")
	case "interrupted":
		_ = appState.finalizeSubmission(sub.ID, "interrupted")
	default:
		_ = appState.finalizeSubmission(sub.ID, "failed")
	}
	terminalText := ""
	attentionUserID := ""
	reuseMessageID := strings.TrimSpace(flush.WorkingMessageID)
	sub = appState.submission(sub.ID)
	if sub != nil {
		newPendingQueueService(a).clearSubmissionProcessingReactions(sub)
		slog.Debug("submission finalized",
			"submission_id", sub.ID,
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
			"status", sub.Status,
		)
		terminalText = turnCompletionTerminalText(sub.Status, flush.LastError)
		attentionUserID = a.turnStopAttentionUserID(sub, turnID)
		if sub.Status == "completed" && !flush.SawFinal {
			a.sendEmptyFinalCardWithReuse(context.Background(), sub, newRuntimeStateService(a).turnFinalFooterLines(turnID, time.Now()), reuseMessageID)
			reuseMessageID = ""
		}
	}
	if sess := appState.session(sessionKey); sess != nil {
		logSessionState("finishTurn before session cleanup", sessionKey, sess)
	}
	updatedSess, _ := appState.updateSession(sessionKey, func(sess *state.Session) {
		if sess == nil {
			return
		}
		sessionRemoveActiveOperation(sess, sub.ID, turnID)
		switch {
		case sessionHasActiveOperations(sess):
			sess.Status = "turn_starting"
			for _, op := range sess.ActiveOperations {
				if strings.TrimSpace(op.TurnID) != "" {
					sess.Status = "turn_in_progress"
					break
				}
			}
		case len(sess.Queue) > 0 || len(sess.StagedImages) > 0:
			sess.Status = "queued"
		default:
			sess.Status = "idle"
		}
	})
	suppressTerminalCard := false
	if updatedSess != nil {
		logSessionState("finishTurn after session cleanup", sessionKey, updatedSess)
		suppressTerminalCard = newAutoRetryService(a).observeAutoRetryTerminal(sessionKey, threadID, sub.Status, updatedSess, sub, reuseMessageID)
	}
	if terminalText != "" && !suppressTerminalCard {
		newOutboundCardService(a).replaceTurnEventCardWithReuse(
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
	if updatedSess != nil && sessionShouldStartNextSubmissionAsync(updatedSess) {
		slog.Debug("finishTurn scheduling next submission asynchronously",
			"session_key", sessionKey,
			"thread_id", updatedSess.ActiveThreadID,
		)
		a.runAsync(func() {
			w.startNextSubmissionAsync(sessionKey, "finishTurn")
		})
	}
	newRuntimeMaintenanceService(a).cleanupSubmissionRuntimeState(sub)
}
