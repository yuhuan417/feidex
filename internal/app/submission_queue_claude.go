package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/state"
)

func (w *submissionWorkflow) startNextClaudeSubmissionWithFailureNotice(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	a := w.app
	appState := a.appState()

	threadID := strings.TrimSpace(sess.ActiveThreadID)
	if !sessionCanResumeThreadForSubmission(sess, sub) {
		if threadID != "" {
			slog.Debug("dropping Claude session lineage for new submission",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"submission_workspace_id", sub.WorkspaceID,
				"active_thread_id", sess.ActiveThreadID,
				"active_thread_workspace_id", sess.ActiveThreadWorkspaceID,
			)
		}
		threadID = ""
		clearSessionThreadContext(sess)
	}

	prompt := buildClaudePrompt(sub)
	if strings.TrimSpace(prompt) == "" {
		err := fmt.Errorf("submission %q has no input", sub.ID)
		w.handleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
		return err
	}

	model := firstNonEmpty(strings.TrimSpace(sess.ModelOverride), strings.TrimSpace(ws.Model), strings.TrimSpace(a.cfg.Claude.Model))
	ensureCtx, ensureCancel := context.WithTimeout(context.Background(), 30*time.Second)
	resumeThreadID := threadID
	claudeThreadID, err := a.claude.EnsureSession(ensureCtx, sessionKey, ws, resumeThreadID, model)
	ensureCancel()
	if err != nil && resumeThreadID != "" {
		slog.Warn("Claude session resume failed; starting fresh session",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", resumeThreadID,
			"workspace_id", sub.WorkspaceID,
			"error", err,
		)
		clearSessionThreadContext(sess)
		if saveErr := appState.saveSession(sess); saveErr != nil {
			return saveErr
		}
		ensureCtx, ensureCancel = context.WithTimeout(context.Background(), 30*time.Second)
		claudeThreadID, err = a.claude.EnsureSession(ensureCtx, sessionKey, ws, "", model)
		ensureCancel()
	}
	if err != nil {
		w.handleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
		slog.Error("Claude session ensure failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"cwd", ws.Cwd,
			"error", err,
		)
		return err
	}

	turnID, err := appState.nextLocalID("claude-turn")
	if err != nil || strings.TrimSpace(turnID) == "" {
		if err == nil {
			err = fmt.Errorf("failed to allocate Claude turn id")
		}
		w.handleSubmissionStartFailure(sessionKey, claudeThreadID, sub, err, notifyFailure)
		return err
	}

	setSessionThreadContext(sess, sub.WorkspaceID, claudeThreadID, firstNonEmpty(strings.TrimSpace(sess.ActiveThreadName), "Claude"), firstNonEmpty(strings.TrimSpace(sess.ActiveThreadPreview), truncate(sub.InputText, 48)))
	updatedSess, err := appState.updateSession(sessionKey, func(current *state.Session) {
		if current == nil {
			return
		}
		setSessionThreadContext(current, sub.WorkspaceID, claudeThreadID, firstNonEmpty(strings.TrimSpace(current.ActiveThreadName), "Claude"), firstNonEmpty(strings.TrimSpace(current.ActiveThreadPreview), truncate(sub.InputText, 48)))
		op := state.SessionActiveOperation{
			Kind:         sessionOpKindSubmission,
			SubmissionID: sub.ID,
			ThreadID:     claudeThreadID,
			TurnID:       turnID,
		}
		if sessionHasActiveOperations(current) {
			sessionPrependActiveOperation(current, op)
		} else {
			sessionUpsertActiveOperation(current, op)
		}
		current.Status = "turn_in_progress"
	})
	if err != nil {
		w.handleSubmissionStartFailure(sessionKey, claudeThreadID, sub, err, notifyFailure)
		return err
	}
	sub.ThreadID = claudeThreadID
	sub.TurnID = turnID
	sub.Status = "running"

	a.bindTurnSubmission(claudeThreadID, turnID, sessionKey, sub.ID)
	a.markTurnStartedAt(turnID, time.Now())
	a.markSubmissionRunningReactions(sub)

	if err := appState.markSubmissionRunning(sub.ID, claudeThreadID, turnID); err != nil {
		w.handleSubmissionStartFailure(sessionKey, claudeThreadID, sub, err, notifyFailure)
		return err
	}
	a.recordSubmissionSourceLinks(sub)
	rootMessageID := ""
	if updatedSess != nil {
		rootMessageID = strings.TrimSpace(updatedSess.RootMessageID)
	}
	a.recordRootTurnBinding(rootMessageID, sessionKey, claudeThreadID, turnID)
	a.noteTurnStarted(sessionKey, sub)
	a.markSessionThreadLive(sessionKey, claudeThreadID)

	turnCtx, turnCancel := context.WithTimeout(context.Background(), 20*time.Second)
	err = a.claude.StartTurn(turnCtx, sessionKey, claudeThreadID, turnID, prompt)
	turnCancel()
	if err != nil {
		w.handleSubmissionStartFailure(sessionKey, claudeThreadID, sub, err, notifyFailure)
		slog.Error("Claude turn start failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", claudeThreadID,
			"workspace_id", sub.WorkspaceID,
			"error", err,
		)
		return err
	}

	slog.Debug("Claude turn started",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", claudeThreadID,
		"turn_id", turnID,
	)
	return nil
}
