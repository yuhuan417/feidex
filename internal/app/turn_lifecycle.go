package app

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

func (w *submissionWorkflow) onTurnStartedNotification(threadID, turnID string) {
	a := w.app
	appState := a.appState()
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return
	}

	if sessionKey, sub := a.pendingSubmissionForThread(threadID); sub != nil {
		a.bindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
		a.markTurnStartedAt(turnID, time.Now())
		a.clearPendingTurnBinding(threadID)

		sess := appState.session(sessionKey)
		if sess == nil {
			return
		}
		sess.ActiveSubmissionID = sub.ID
		sess.ActiveTurnID = turnID
		sess.Status = "turn_in_progress"
		setSessionThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
		if err := appState.saveSession(sess); err != nil {
			return
		}
		_ = appState.markSubmissionRunning(sub.ID, threadID, turnID)
		sub.ThreadID = threadID
		sub.TurnID = turnID
		sub.Status = "running"
		a.recordSubmissionSourceLinks(sub)
		a.recordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
		a.noteTurnStarted(sessionKey, sub)
		a.markSessionThreadLive(sessionKey, threadID)
		return
	}
	if a.bindStandaloneCompactTurn(threadID, turnID) {
		return
	}

	sessionKey := ""
	for _, candidate := range appState.sessions() {
		if candidate == nil {
			continue
		}
		if strings.TrimSpace(candidate.ActiveThreadID) != threadID {
			continue
		}
		if strings.TrimSpace(candidate.ActiveTurnID) == turnID {
			return
		}
		if strings.TrimSpace(candidate.ActiveTurnID) != "" {
			continue
		}
		if strings.TrimSpace(candidate.ActiveSubmissionID) == "" {
			continue
		}
		sessionKey = candidate.Key
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
	sub := appState.submission(sess.ActiveSubmissionID)
	if sub == nil {
		slog.Warn("turn started notification missing submission",
			"session_key", sessionKey,
			"submission_id", sess.ActiveSubmissionID,
			"thread_id", threadID,
			"turn_id", turnID,
		)
		return
	}
	sess.ActiveSubmissionID = sub.ID
	sess.ActiveTurnID = turnID
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
	a.bindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	a.markTurnStartedAt(turnID, time.Now())
	a.recordSubmissionSourceLinks(sub)
	a.recordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
	a.noteTurnStarted(sessionKey, sub)
	a.markSessionThreadLive(sessionKey, threadID)
	slog.Debug("turn started notification rebound pending submission",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	logSessionState("turn started notification session snapshot", sessionKey, appState.session(sessionKey))
}

func (w *submissionWorkflow) finishTurn(threadID, turnID, status string) {
	a := w.app
	appState := a.appState()
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
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

	flush := a.flushTurnStream(context.Background(), threadID, turnID)

	switch status {
	case "completed":
		_ = appState.finalizeSubmission(sub.ID, "completed")
	case "interrupted":
		_ = appState.finalizeSubmission(sub.ID, "interrupted")
	default:
		_ = appState.finalizeSubmission(sub.ID, "failed")
	}
	sub = appState.submission(sub.ID)
	if sub != nil {
		a.clearSubmissionProcessingReactions(sub)
		slog.Debug("submission finalized",
			"submission_id", sub.ID,
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
			"status", sub.Status,
		)
		replyText, terminalText := turnCompletionMessages(sub.Status, sub.OutputText, flush.LastError, flush.SentOutput)
		if sub.Status == "completed" && flush.SawFinal {
			replyText = a.turnFinalText(turnID)
		}
		if replyText != "" {
			_, contextLine, elapsedLine := a.turnFinalMetadata(turnID, time.Now())
			a.sendFinalMessagesWithFooter(context.Background(), sub, replyText, []string{contextLine, elapsedLine}, a.replyInThreadForSubmission(sub))
			flush.SentOutput = true
		}
		if sub.Status == "completed" && !flush.SawFinal {
			_, contextLine, elapsedLine := a.turnFinalMetadata(turnID, time.Now())
			a.sendEmptyFinalCard(context.Background(), sub, []string{contextLine, elapsedLine})
		}
		if terminalText != "" {
			a.sendTurnEventMessages(context.Background(), sub, terminalText, a.replyInThreadForSubmission(sub), "turn_terminal")
		}
	}
	sess := appState.session(sessionKey)
	if sess != nil {
		logSessionState("finishTurn before session clear", sessionKey, sess)
		sess.ActiveTurnID = ""
		sess.ActiveSubmissionID = ""
		sess.Status = "idle"
		_ = appState.saveSession(sess)
		logSessionState("finishTurn after session clear", sessionKey, appState.session(sessionKey))
		slog.Debug("finishTurn scheduling next submission asynchronously",
			"session_key", sessionKey,
			"thread_id", sess.ActiveThreadID,
		)
		go w.startNextSubmissionAsync(sessionKey, "finishTurn")
	}
	a.cleanupSubmissionRuntimeState(sub)
}
