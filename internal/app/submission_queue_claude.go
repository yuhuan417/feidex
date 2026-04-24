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
	if a == nil || a.claude == nil {
		err := fmt.Errorf("claude backend not initialized")
		threadID := ""
		if sess != nil {
			threadID = strings.TrimSpace(sess.ActiveThreadID)
		}
		w.handleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
		slog.Warn("Claude submission start skipped because runtime is unavailable",
			"session_key", sessionKey,
			"submission_id", func() string {
				if sub == nil {
					return ""
				}
				return sub.ID
			}(),
			"thread_id", threadID,
			"workspace_id", func() string {
				if sub == nil {
					return ""
				}
				return sub.WorkspaceID
			}(),
		)
		return err
	}

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

	updatedSess, turnID, err := w.startClaudeSubmissionAttempt(sessionKey, sess, sub, claudeThreadID, prompt)
	if err != nil && strings.TrimSpace(resumeThreadID) != "" {
		slog.Warn("Claude resumed session turn start failed; retrying fresh session",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"stale_thread_id", resumeThreadID,
			"workspace_id", sub.WorkspaceID,
			"error", err,
		)
		sess, sub, err = w.rollbackClaudeSubmissionStartState(sessionKey, sub, turnID)
		ensureCtx, ensureCancel = context.WithTimeout(context.Background(), 30*time.Second)
		claudeThreadID, err = a.claude.EnsureSession(ensureCtx, sessionKey, ws, "", model)
		ensureCancel()
		if err == nil {
			if sess == nil {
				sess = appState.session(sessionKey)
			}
			if sess == nil {
				err = fmt.Errorf("session %q disappeared during Claude retry", sessionKey)
			}
		}
		if err == nil {
			if sub == nil {
				err = fmt.Errorf("submission disappeared during Claude retry")
			} else {
				updatedSess, turnID, err = w.startClaudeSubmissionAttempt(sessionKey, sess, sub, claudeThreadID, prompt)
			}
		}
	}
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
	_ = updatedSess
	return nil
}

func (w *submissionWorkflow) startClaudeSubmissionAttempt(sessionKey string, sess *state.Session, sub *state.Submission, claudeThreadID, prompt string) (*state.Session, string, error) {
	a := w.app
	appState := a.appState()
	if a == nil || a.claude == nil {
		return nil, "", fmt.Errorf("claude backend not initialized")
	}

	turnID, err := appState.nextLocalID("claude-turn")
	if err != nil || strings.TrimSpace(turnID) == "" {
		if err == nil {
			err = fmt.Errorf("failed to allocate Claude turn id")
		}
		return nil, "", err
	}

	updatedSess, err := w.bindClaudeSubmissionStartState(sessionKey, sess, sub, claudeThreadID, turnID)
	if err != nil {
		return nil, turnID, err
	}
	a.markTurnStartedAt(turnID, time.Now())
	a.markSubmissionRunningReactions(sub)

	turnCtx, turnCancel := context.WithTimeout(context.Background(), 20*time.Second)
	err = a.claude.StartTurn(turnCtx, sessionKey, claudeThreadID, turnID, prompt)
	turnCancel()
	if err != nil {
		return updatedSess, turnID, err
	}
	return updatedSess, turnID, nil
}

func (w *submissionWorkflow) rollbackClaudeSubmissionStartState(sessionKey string, sub *state.Submission, turnID string) (*state.Session, *state.Submission, error) {
	a := w.app
	appState := a.appState()
	submissionID := ""
	if sub != nil {
		submissionID = strings.TrimSpace(sub.ID)
	}

	updatedSess, err := appState.updateSession(sessionKey, func(current *state.Session) {
		if current == nil {
			return
		}
		if submissionID != "" {
			sessionRemoveActiveOperation(current, submissionID, turnID)
		}
		switch {
		case sessionHasActiveOperations(current):
			current.Status = "turn_starting"
			for _, op := range current.ActiveOperations {
				if strings.TrimSpace(op.TurnID) != "" {
					current.Status = "turn_in_progress"
					break
				}
			}
		case len(current.Queue) > 0 || len(current.StagedImages) > 0:
			clearSessionThreadContext(current)
			current.Status = "queued"
		default:
			clearSessionThreadContext(current)
			current.Status = "idle"
		}
	})
	if err != nil {
		return nil, nil, err
	}

	var refreshedSub *state.Submission
	if submissionID != "" {
		if err := appState.updateSubmission(submissionID, func(current *state.Submission) {
			if current == nil {
				return
			}
			current.ThreadID = ""
			current.TurnID = ""
			current.Status = "queued"
			current.Finalized = false
		}); err != nil {
			return updatedSess, nil, err
		}
		refreshedSub = appState.submission(submissionID)
	}

	if strings.TrimSpace(turnID) != "" {
		appState.deletePendingRequests(func(req *state.PendingRequest) bool {
			return req != nil && strings.TrimSpace(req.TurnID) == strings.TrimSpace(turnID)
		})
		appState.deleteMessageLinks(func(link *state.MessageLink) bool {
			return link != nil && strings.TrimSpace(link.TurnID) == strings.TrimSpace(turnID)
		})
		a.clearTurnBinding(turnID)
		a.clearTurnItemStates(turnID)
		a.deleteTurnStream(turnID)
	}

	if updatedSess == nil || !sessionHasActiveOperations(updatedSess) {
		a.clearSessionLiveThread(sessionKey)
	}
	return updatedSess, refreshedSub, nil
}

func (w *submissionWorkflow) bindClaudeSubmissionStartState(sessionKey string, sess *state.Session, sub *state.Submission, claudeThreadID, turnID string) (*state.Session, error) {
	a := w.app
	appState := a.appState()
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
		return nil, err
	}
	sub.ThreadID = claudeThreadID
	sub.TurnID = turnID
	sub.Status = "running"
	a.bindTurnSubmission(claudeThreadID, turnID, sessionKey, sub.ID)
	if err := appState.markSubmissionRunning(sub.ID, claudeThreadID, turnID); err != nil {
		return nil, err
	}
	a.recordSubmissionSourceLinks(sub)
	rootMessageID := ""
	if updatedSess != nil {
		rootMessageID = strings.TrimSpace(updatedSess.RootMessageID)
	}
	a.recordRootTurnBinding(rootMessageID, sessionKey, claudeThreadID, turnID)
	a.noteTurnStarted(sessionKey, sub)
	if strings.TrimSpace(claudeThreadID) != "" {
		a.markSessionThreadLive(sessionKey, claudeThreadID)
	} else {
		a.clearSessionLiveThread(sessionKey)
	}
	return updatedSess, nil
}
