package submission

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/app/attachments"
	"feidex/internal/app/sessionctx"
	"feidex/internal/config"
	"feidex/internal/state"
)

// StartNextClaudeSubmissionWithFailureNotice is a convenience wrapper that
// starts a non-steer Claude submission.
func (s SubmissionQueueService) StartNextClaudeSubmissionWithFailureNotice(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	return s.StartNextClaudeSubmissionWithFailureNoticeEx(sessionKey, sess, sub, ws, notifyFailure, false)
}

// StartNextClaudeSubmissionWithFailureNoticeEx handles Claude-specific
// submission startup: session resume, prompt build, EnsureSession with
// retry, and startClaudeSubmissionAttempt with fallback.
func (s SubmissionQueueService) StartNextClaudeSubmissionWithFailureNoticeEx(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure, steer bool) error {
	a := s.App
	appState := a.SubmissionQueueAppState()
	claude := a.SubmissionQueueClaudeClient()
	if claude == nil {
		err := fmt.Errorf("claude backend not initialized")
		threadID := ""
		if sess != nil {
			threadID = strings.TrimSpace(sess.ActiveThreadID)
		}
		s.HandleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
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
	if !sessionctx.CanResumeThreadForSubmission(sess, sub) {
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
		sessionctx.ClearThreadContext(sess)
	}

	prompt := buildClaudePrompt(sub)
	if strings.TrimSpace(prompt) == "" {
		err := fmt.Errorf("submission %q has no input", sub.ID)
		s.HandleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
		return err
	}

	model := effectiveClaudeModel(a, sess, sub, ws)
	ensureCtx, ensureCancel := context.WithTimeout(context.Background(), 30*time.Second)
	resumeThreadID := threadID
	claudeThreadID, err := claude.EnsureSession(ensureCtx, sessionKey, ws, resumeThreadID, model)
	ensureCancel()
	if err != nil && resumeThreadID != "" {
		slog.Warn("Claude session resume failed; starting fresh session",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", resumeThreadID,
			"workspace_id", sub.WorkspaceID,
			"error", err,
		)
		sessionctx.ClearThreadContext(sess)
		if saveErr := appState.SaveSession(sess); saveErr != nil {
			return saveErr
		}
		ensureCtx, ensureCancel = context.WithTimeout(context.Background(), 30*time.Second)
		claudeThreadID, err = claude.EnsureSession(ensureCtx, sessionKey, ws, "", model)
		ensureCancel()
	}
	if err != nil {
		s.HandleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
		slog.Error("Claude session ensure failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"cwd", ws.Cwd,
			"error", err,
		)
		return err
	}

	updatedSess, turnID, err := s.startClaudeSubmissionAttempt(claude, sessionKey, sess, sub, claudeThreadID, prompt, steer)
	if err != nil && strings.TrimSpace(resumeThreadID) != "" {
		slog.Warn("Claude resumed session turn start failed; retrying fresh session",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"stale_thread_id", resumeThreadID,
			"workspace_id", sub.WorkspaceID,
			"error", err,
		)
		sess, sub, err = s.rollbackClaudeSubmissionStartState(sessionKey, sub, turnID)
		ensureCtx, ensureCancel = context.WithTimeout(context.Background(), 30*time.Second)
		claudeThreadID, err = claude.EnsureSession(ensureCtx, sessionKey, ws, "", model)
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
				updatedSess, turnID, err = s.startClaudeSubmissionAttempt(claude, sessionKey, sess, sub, claudeThreadID, prompt, steer)
			}
		}
	}
	if err != nil {
		s.HandleSubmissionStartFailure(sessionKey, claudeThreadID, sub, err, notifyFailure)
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

func (s SubmissionQueueService) startClaudeSubmissionAttempt(claude QueueClaudeClient, sessionKey string, sess *state.Session, sub *state.Submission, claudeThreadID, prompt string, steer bool) (*state.Session, string, error) {
	a := s.App
	appState := a.SubmissionQueueAppState()

	if steer {
		return s.startSteerSubmissionAttempt(claude, sessionKey, sess, sub, claudeThreadID, prompt)
	}

	turnID, err := appState.NextLocalID("claude-turn")
	if err != nil || strings.TrimSpace(turnID) == "" {
		if err == nil {
			err = fmt.Errorf("failed to allocate Claude turn id")
		}
		return nil, "", err
	}

	updatedSess, err := s.bindClaudeSubmissionStartState(sessionKey, sess, sub, claudeThreadID, turnID)
	if err != nil {
		return nil, turnID, err
	}
	a.SubmissionQueueRuntimeState().MarkTurnStartedAt(turnID, time.Now())
	a.SubmissionQueueMarkSubmissionRunningReactions(sub)

	turnCtx, turnCancel := context.WithTimeout(context.Background(), 20*time.Second)
	err = claude.StartTurn(turnCtx, sessionKey, claudeThreadID, turnID, prompt)
	turnCancel()
	if err != nil {
		return updatedSess, turnID, err
	}
	return updatedSess, turnID, nil
}

// startSteerSubmissionAttempt sends a steer message into the current
// conversation without creating a separate CLI turn.
func (s SubmissionQueueService) startSteerSubmissionAttempt(claude QueueClaudeClient, sessionKey string, sess *state.Session, sub *state.Submission, claudeThreadID, prompt string) (*state.Session, string, error) {
	a := s.App
	appState := a.SubmissionQueueAppState()

	turnID, err := appState.NextLocalID("claude-turn")
	if err != nil || strings.TrimSpace(turnID) == "" {
		if err == nil {
			err = fmt.Errorf("failed to allocate Claude steer turn id")
		}
		return nil, "", err
	}

	updatedSess, err := s.bindClaudeSubmissionStartState(sessionKey, sess, sub, claudeThreadID, turnID)
	if err != nil {
		return nil, turnID, err
	}
	a.SubmissionQueueMarkSubmissionRunningReactions(sub)

	turnCtx, turnCancel := context.WithTimeout(context.Background(), 20*time.Second)
	err = claude.StartSteerTurn(turnCtx, sessionKey, claudeThreadID, turnID, prompt, sub.ID)
	turnCancel()
	if err != nil {
		return updatedSess, turnID, err
	}
	return updatedSess, turnID, nil
}

func (s SubmissionQueueService) rollbackClaudeSubmissionStartState(sessionKey string, sub *state.Submission, turnID string) (*state.Session, *state.Submission, error) {
	a := s.App
	appState := a.SubmissionQueueAppState()
	submissionID := ""
	if sub != nil {
		submissionID = strings.TrimSpace(sub.ID)
	}

	updatedSess, err := appState.UpdateSession(sessionKey, func(current *state.Session) {
		if current == nil {
			return
		}
		if submissionID != "" {
			sessionctx.RemoveActiveOperation(current, submissionID, turnID)
		}
		switch {
		case sessionctx.HasActiveOperations(current):
			current.Status = state.SessionStatusTurnStarting.String()
			for _, op := range current.ActiveOperations {
				if strings.TrimSpace(op.TurnID) != "" {
					current.Status = state.SessionStatusTurnInProgress.String()
					break
				}
			}
		case len(current.Queue) > 0 || len(current.StagedImages) > 0:
			sessionctx.ClearThreadContext(current)
			current.Status = state.SessionStatusQueued.String()
		default:
			sessionctx.ClearThreadContext(current)
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
		a.SubmissionQueueRuntimeState().ClearTurnBinding(turnID)
		a.SubmissionQueueRuntimeState().ClearTurnItemStates(turnID)
		a.SubmissionQueueTurnStream().DeleteTurnStream(turnID)
	}

	if updatedSess == nil || !sessionctx.HasActiveOperations(updatedSess) {
		a.SubmissionQueueLiveThread().ClearSessionLiveThread(sessionKey)
	}
	return updatedSess, refreshedSub, nil
}

func (s SubmissionQueueService) bindClaudeSubmissionStartState(sessionKey string, sess *state.Session, sub *state.Submission, claudeThreadID, turnID string) (*state.Session, error) {
	a := s.App
	appState := a.SubmissionQueueAppState()
	sessionctx.SetThreadContext(sess, sub.WorkspaceID, claudeThreadID, firstNonEmpty(strings.TrimSpace(sess.ActiveThreadName), "Claude"), firstNonEmpty(strings.TrimSpace(sess.ActiveThreadPreview), truncate(sub.InputText, 48)))
	updatedSess, err := appState.UpdateSession(sessionKey, func(current *state.Session) {
		if current == nil {
			return
		}
		sessionctx.SetThreadContext(current, sub.WorkspaceID, claudeThreadID, firstNonEmpty(strings.TrimSpace(current.ActiveThreadName), "Claude"), firstNonEmpty(strings.TrimSpace(current.ActiveThreadPreview), truncate(sub.InputText, 48)))
		op := state.SessionActiveOperation{
			Kind:         sessionctx.OpKindSubmission,
			SubmissionID: sub.ID,
			ThreadID:     claudeThreadID,
			TurnID:       turnID,
		}
		if sessionctx.HasActiveOperations(current) {
			sessionctx.PrependActiveOperation(current, op)
		} else {
			sessionctx.UpsertActiveOperation(current, op)
		}
		current.Status = state.SessionStatusTurnInProgress.String()
	})
	if err != nil {
		return nil, err
	}
	sub.ThreadID = claudeThreadID
	sub.TurnID = turnID
	sub.Status = state.SubmissionStatusRunning.String()
	a.SubmissionQueueRuntimeState().BindTurnSubmission(claudeThreadID, turnID, sessionKey, sub.ID)
	if err := appState.MarkSubmissionRunning(sub.ID, claudeThreadID, turnID); err != nil {
		return nil, err
	}
	a.SubmissionQueueReplyContinuation().RecordSubmissionSourceLinks(sub)
	rootMessageID := ""
	if updatedSess != nil {
		rootMessageID = strings.TrimSpace(updatedSess.RootMessageID)
	}
	a.SubmissionQueueReplyContinuation().RecordRootTurnBinding(rootMessageID, sessionKey, claudeThreadID, turnID)
	a.SubmissionQueueTurnStream().NoteTurnStarted(sessionKey, sub)
	if strings.TrimSpace(claudeThreadID) != "" {
		a.SubmissionQueueLiveThread().MarkSessionThreadLive(sessionKey, claudeThreadID)
	} else {
		a.SubmissionQueueLiveThread().ClearSessionLiveThread(sessionKey)
	}
	return updatedSess, nil
}

// buildClaudePrompt builds the prompt text for a Claude submission from
// skills, input text, and attachments.
func buildClaudePrompt(sub *state.Submission) string {
	if sub == nil {
		return ""
	}
	parts := make([]string, 0, len(sub.Skills)+1+len(sub.Attachments))
	for _, skill := range sub.Skills {
		if strings.TrimSpace(skill.Name) == "" && strings.TrimSpace(skill.Path) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("Use skill `%s` (`%s`) if it is available in this Claude session.", firstNonEmpty(strings.TrimSpace(skill.Name), "skill"), firstNonEmpty(strings.TrimSpace(skill.Path), "-")))
	}
	if text := strings.TrimSpace(sub.InputText); text != "" {
		parts = append(parts, text)
	}
	for _, attachment := range sub.Attachments {
		if prompt := attachments.AttachmentPrompt(attachment); prompt != "" {
			parts = append(parts, prompt)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// truncate truncates s to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
