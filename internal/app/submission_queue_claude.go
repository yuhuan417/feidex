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

// claudeSubmissionService handles Claude-specific submission startup, retry,
// and rollback. The core queue management is delegated to the submission
// sub-package; this file retains the Claude-specific turn lifecycle.
type claudeSubmissionService struct {
	app *App
}

func newClaudeSubmissionService(app *App) *claudeSubmissionService {
	return &claudeSubmissionService{app: app}
}

func (w *claudeSubmissionService) startNextClaudeSubmissionWithFailureNotice(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	return w.startNextClaudeSubmissionWithFailureNoticeEx(sessionKey, sess, sub, ws, notifyFailure, false)
}

func (w *claudeSubmissionService) startNextClaudeSubmissionWithFailureNoticeEx(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure, steer bool) error {
	a := w.app
	appState := a.State()
	if a == nil || a.claude == nil {
		err := fmt.Errorf("claude backend not initialized")
		threadID := ""
		if sess != nil {
			threadID = strings.TrimSpace(sess.ActiveThreadID)
		}
		newSubmissionCoordinator(w.app).handleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
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
		newSubmissionCoordinator(w.app).handleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
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
		if saveErr := appState.SaveSession(sess); saveErr != nil {
			return saveErr
		}
		ensureCtx, ensureCancel = context.WithTimeout(context.Background(), 30*time.Second)
		claudeThreadID, err = a.claude.EnsureSession(ensureCtx, sessionKey, ws, "", model)
		ensureCancel()
	}
	if err != nil {
		newSubmissionCoordinator(w.app).handleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
		slog.Error("Claude session ensure failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"cwd", ws.Cwd,
			"error", err,
		)
		return err
	}

	updatedSess, turnID, err := w.startClaudeSubmissionAttempt(sessionKey, sess, sub, claudeThreadID, prompt, steer)
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
				sess = appState.Session(sessionKey)
			}
			if sess == nil {
				err = fmt.Errorf("session %q disappeared during Claude retry", sessionKey)
			}
		}
		if err == nil {
			if sub == nil {
				err = fmt.Errorf("submission disappeared during Claude retry")
			} else {
				updatedSess, turnID, err = w.startClaudeSubmissionAttempt(sessionKey, sess, sub, claudeThreadID, prompt, steer)
			}
		}
	}
	if err != nil {
		newSubmissionCoordinator(w.app).handleSubmissionStartFailure(sessionKey, claudeThreadID, sub, err, notifyFailure)
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

func (w *claudeSubmissionService) startClaudeSubmissionAttempt(sessionKey string, sess *state.Session, sub *state.Submission, claudeThreadID, prompt string, steer bool) (*state.Session, string, error) {
	a := w.app
	appState := a.State()
	if a == nil || a.claude == nil {
		return nil, "", fmt.Errorf("claude backend not initialized")
	}

	if steer {
		return w.startSteerSubmissionAttempt(sessionKey, sess, sub, claudeThreadID, prompt)
	}

	turnID, err := appState.NextLocalID("claude-turn")
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
	newRuntimeStateService(a).markTurnStartedAt(turnID, time.Now())
	newPendingQueueService(a).markSubmissionRunningReactions(sub)

	turnCtx, turnCancel := context.WithTimeout(context.Background(), 20*time.Second)
	err = a.claude.StartTurn(turnCtx, sessionKey, claudeThreadID, turnID, prompt)
	turnCancel()
	if err != nil {
		return updatedSess, turnID, err
	}
	return updatedSess, turnID, nil
}

// startSteerSubmissionAttempt sends a steer message into the current
// conversation without creating a separate CLI turn. The steer submission
// gets its own turnID for tracking but the message is sent via SendSteerInput
// (not SendMessage), so no separate turn is created in the CLI session.
// The steer submission is finalized together with the current turn via
// the SteerSubmissionID recorded on the current TurnState.
func (w *claudeSubmissionService) startSteerSubmissionAttempt(sessionKey string, sess *state.Session, sub *state.Submission, claudeThreadID, prompt string) (*state.Session, string, error) {
	a := w.app
	appState := a.State()

	turnID, err := appState.NextLocalID("claude-turn")
	if err != nil || strings.TrimSpace(turnID) == "" {
		if err == nil {
			err = fmt.Errorf("failed to allocate Claude steer turn id")
		}
		return nil, "", err
	}

	updatedSess, err := w.bindClaudeSubmissionStartState(sessionKey, sess, sub, claudeThreadID, turnID)
	if err != nil {
		return nil, turnID, err
	}
	newPendingQueueService(a).markSubmissionRunningReactions(sub)

	turnCtx, turnCancel := context.WithTimeout(context.Background(), 20*time.Second)
	err = a.claude.StartSteerTurn(turnCtx, sessionKey, claudeThreadID, turnID, prompt, sub.ID)
	turnCancel()
	if err != nil {
		return updatedSess, turnID, err
	}
	return updatedSess, turnID, nil
}

func (w *claudeSubmissionService) rollbackClaudeSubmissionStartState(sessionKey string, sub *state.Submission, turnID string) (*state.Session, *state.Submission, error) {
	a := w.app
	appState := a.State()
	submissionID := ""
	if sub != nil {
		submissionID = strings.TrimSpace(sub.ID)
	}

	updatedSess, err := appState.UpdateSession(sessionKey, func(current *state.Session) {
		if current == nil {
			return
		}
		if submissionID != "" {
			sessionRemoveActiveOperation(current, submissionID, turnID)
		}
		switch {
		case sessionHasActiveOperations(current):
			current.Status = state.SessionStatusTurnStarting.String()
			for _, op := range current.ActiveOperations {
				if strings.TrimSpace(op.TurnID) != "" {
					current.Status = state.SessionStatusTurnInProgress.String()
					break
				}
			}
		case len(current.Queue) > 0 || len(current.StagedImages) > 0:
			clearSessionThreadContext(current)
			current.Status = state.SessionStatusQueued.String()
		default:
			clearSessionThreadContext(current)
			current.Status = state.SessionStatusIdle.String()
		}
	})
	if err != nil {
		return nil, nil, err
	}

	var refreshedSub *state.Submission
	if submissionID != "" {
		if err := appState.UpdateSubmission(submissionID, func(current *state.Submission) {
			if current == nil {
				return
			}
			current.ThreadID = ""
			current.TurnID = ""
			current.Status = state.SessionStatusQueued.String()
			current.Finalized = false
		}); err != nil {
			return updatedSess, nil, err
		}
		refreshedSub = appState.Submission(submissionID)
	}

	if strings.TrimSpace(turnID) != "" {
		appState.DeletePendingRequests(func(req *state.PendingRequest) bool {
			return req != nil && strings.TrimSpace(req.TurnID) == strings.TrimSpace(turnID)
		})
		appState.DeleteMessageLinks(func(link *state.MessageLink) bool {
			return link != nil && strings.TrimSpace(link.TurnID) == strings.TrimSpace(turnID)
		})
		newRuntimeStateService(a).clearTurnBinding(turnID)
		newRuntimeStateService(a).clearTurnItemStates(turnID)
		newTurnStreamService(a).deleteTurnStream(turnID)
	}

	if updatedSess == nil || !sessionHasActiveOperations(updatedSess) {
		clearSessionLiveThread(a, sessionKey)
	}
	return updatedSess, refreshedSub, nil
}

func (w *claudeSubmissionService) bindClaudeSubmissionStartState(sessionKey string, sess *state.Session, sub *state.Submission, claudeThreadID, turnID string) (*state.Session, error) {
	a := w.app
	appState := a.State()
	setSessionThreadContext(sess, sub.WorkspaceID, claudeThreadID, firstNonEmpty(strings.TrimSpace(sess.ActiveThreadName), "Claude"), firstNonEmpty(strings.TrimSpace(sess.ActiveThreadPreview), truncate(sub.InputText, 48)))
	updatedSess, err := appState.UpdateSession(sessionKey, func(current *state.Session) {
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
		current.Status = state.SessionStatusTurnInProgress.String()
	})
	if err != nil {
		return nil, err
	}
	sub.ThreadID = claudeThreadID
	sub.TurnID = turnID
	sub.Status = state.SubmissionStatusRunning.String()
	newRuntimeStateService(a).bindTurnSubmission(claudeThreadID, turnID, sessionKey, sub.ID)
	if err := appState.MarkSubmissionRunning(sub.ID, claudeThreadID, turnID); err != nil {
		return nil, err
	}
	newReplyContinuationService(a).recordSubmissionSourceLinks(sub)
	rootMessageID := ""
	if updatedSess != nil {
		rootMessageID = strings.TrimSpace(updatedSess.RootMessageID)
	}
	newReplyContinuationService(a).recordRootTurnBinding(rootMessageID, sessionKey, claudeThreadID, turnID)
	newTurnStreamService(a).noteTurnStarted(sessionKey, sub)
	if strings.TrimSpace(claudeThreadID) != "" {
		markSessionThreadLive(a, sessionKey, claudeThreadID)
	} else {
		clearSessionLiveThread(a, sessionKey)
	}
	return updatedSess, nil
}
