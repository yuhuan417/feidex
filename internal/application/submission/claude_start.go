package submission

import (
	"context"
	"errors"
	"feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/domain/workspace"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// StartNextClaudeSubmissionWithFailureNotice is a convenience wrapper that
// starts a non-steer Claude submission.
func (s SubmissionQueueService) StartNextClaudeSubmissionWithFailureNotice(sessionKey string, sess *conversation.Session, sub *domainsubmission.Submission, ws *workspace.Workspace, notifyFailure bool) error {
	return s.StartNextClaudeSubmissionWithFailureNoticeEx(sessionKey, sess, sub, ws, notifyFailure, false)
}

// StartNextClaudeSubmissionWithFailureNoticeEx handles Claude-specific
// submission startup: session resume, prompt build, EnsureSession with
// retry, and startClaudeSubmissionAttempt with fallback.
func (s SubmissionQueueService) StartNextClaudeSubmissionWithFailureNoticeEx(sessionKey string, sess *conversation.Session, sub *domainsubmission.Submission, ws *workspace.Workspace, notifyFailure, steer bool) error {
	a := s.Deps
	appState := a.AppState
	claude := a.ClaudeClient()
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
	if !(sub != nil && conversation.CanResumeThreadForWorkspace(sess, sub.WorkspaceID)) {
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
		conversation.ClearThreadContext(sess)
	}

	prompt := a.ClaudePrompt(sub)
	if strings.TrimSpace(prompt) == "" {
		err := fmt.Errorf("submission %q has no input", sub.ID)
		s.HandleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
		return err
	}

	if steer {
		_, _, err := s.startClaudeSubmissionAttempt(claude, sessionKey, sess, sub, threadID, prompt, true)
		if err != nil {
			s.HandleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
		}
		return err
	}

	snapshot, err := s.modelSnapshot(sess, sub)
	if err != nil {
		return err
	}
	model := snapshot.Model
	ensureCtx, ensureCancel := context.WithTimeout(a.context(), 30*time.Second)
	resumeThreadID := threadID
	claudeThreadID, err := claude.EnsureSession(ensureCtx, sessionKey, ws, resumeThreadID, model)
	ensureCancel()
	if errors.Is(err, backend.ErrModelConfigApply) {
		return s.deferClaudeModelConfig(sessionKey, sub, err, notifyFailure)
	}
	if err != nil && resumeThreadID != "" {
		slog.Warn("Claude session resume failed; starting fresh session",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", resumeThreadID,
			"workspace_id", sub.WorkspaceID,
			"error", err,
		)
		conversation.ClearThreadContext(sess)
		if saveErr := appState.SaveSession(sess); saveErr != nil {
			return saveErr
		}
		ensureCtx, ensureCancel = context.WithTimeout(a.context(), 30*time.Second)
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
	if errors.Is(err, backend.ErrModelConfigApply) {
		if _, _, rollbackErr := s.rollbackClaudeSubmissionStartState(sessionKey, sub, turnID, true); rollbackErr != nil {
			return rollbackErr
		}
		return s.deferClaudeModelConfig(sessionKey, sub, err, notifyFailure)
	}
	if err != nil && strings.TrimSpace(resumeThreadID) != "" && !canRetryFreshClaudeSession(claude, sessionKey) {
		if _, _, rollbackErr := s.rollbackClaudeSubmissionStartState(sessionKey, sub, turnID, true); rollbackErr != nil {
			return rollbackErr
		}
		return s.deferClaudeModelConfig(sessionKey, sub, fmt.Errorf("%w: %v", backend.ErrModelConfigApply, err), notifyFailure)
	}
	if err != nil && strings.TrimSpace(resumeThreadID) != "" && canRetryFreshClaudeSession(claude, sessionKey) {
		slog.Warn("Claude resumed session turn start failed; retrying fresh session",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"stale_thread_id", resumeThreadID,
			"workspace_id", sub.WorkspaceID,
			"error", err,
		)
		var rollbackErr error
		sess, sub, rollbackErr = s.rollbackClaudeSubmissionStartState(sessionKey, sub, turnID, false)
		if rollbackErr != nil {
			s.HandleSubmissionStartFailure(sessionKey, claudeThreadID, sub, rollbackErr, notifyFailure)
			slog.Error("Claude submission rollback failed before fresh session retry",
				"session_key", sessionKey,
				"error", rollbackErr,
			)
			return rollbackErr
		}
		ensureCtx, ensureCancel = context.WithTimeout(a.context(), 30*time.Second)
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

// ContinueClaudeBackgroundWorkflow starts a new Claude CLI turn for the same
// submission after all background agents spawned by its previous turn finish.
func (s SubmissionQueueService) ContinueClaudeBackgroundWorkflow(sessionKey, threadID, submissionID, prompt string) error {
	a := s.Deps
	sess := a.AppState.Session(sessionKey)
	sub := a.AppState.Submission(submissionID)
	if sess == nil || sub == nil || sub.Finalized {
		return fmt.Errorf("background workflow submission is no longer active")
	}
	if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) || !conversation.HasBackgroundWorkflow(sess) {
		return fmt.Errorf("background workflow session state changed before continuation")
	}
	ws := a.Workspace(sub.WorkspaceID)
	if ws == nil {
		return fmt.Errorf("workspace %q not found", sub.WorkspaceID)
	}
	claude := a.ClaudeClient()
	if claude == nil {
		return fmt.Errorf("claude backend not initialized")
	}
	snapshot, err := s.modelSnapshot(sess, sub)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.context(), 30*time.Second)
	resumedThreadID, err := claude.EnsureSession(ctx, sessionKey, ws, threadID, snapshot.Model)
	cancel()
	if err != nil {
		return err
	}
	if strings.TrimSpace(resumedThreadID) != strings.TrimSpace(threadID) {
		return fmt.Errorf("Claude resumed a different session: got %q, want %q", resumedThreadID, threadID)
	}
	_, turnID, err := s.startClaudeSubmissionAttempt(claude, sessionKey, sess, sub, resumedThreadID, strings.TrimSpace(prompt), false)
	if err != nil {
		if strings.TrimSpace(turnID) != "" {
			if _, _, rollbackErr := s.rollbackClaudeSubmissionStartState(sessionKey, sub, turnID, true); rollbackErr != nil {
				return fmt.Errorf("start continuation: %v; rollback: %w", err, rollbackErr)
			}
		}
		return err
	}
	return nil
}

func (s SubmissionQueueService) AddClaudeBackgroundWorkflowSteer(sessionKey, threadID string, sub *domainsubmission.Submission) error {
	if sub == nil || strings.TrimSpace(sub.ID) == "" {
		return fmt.Errorf("background workflow steer submission is missing")
	}
	a := s.Deps
	sess := a.AppState.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) ||
		strings.TrimSpace(sess.ActiveTurnID) != "" || !conversation.HasBackgroundWorkflow(sess) {
		return fmt.Errorf("Claude background workflow is no longer waiting")
	}
	claude := a.ClaudeClient()
	registrar, ok := claude.(interface {
		AddBackgroundWorkflowSteer(sessionKey, threadID string, sub *domainsubmission.Submission) bool
	})
	if !ok {
		return fmt.Errorf("Claude background workflow steer is unavailable")
	}
	sub.ThreadID = strings.TrimSpace(threadID)
	sub.TurnID = ""
	sub.Status = domainsubmission.SubmissionStatusWaitingBackground.String()
	if err := a.AppState.UpdateSubmission(sub.ID, func(current *domainsubmission.Submission) {
		if current == nil || current.Finalized {
			return
		}
		current.ThreadID = sub.ThreadID
		current.TurnID = ""
		current.Status = sub.Status
	}); err != nil {
		return err
	}
	_, err := a.AppState.UpdateSession(sessionKey, func(current *conversation.Session) {
		conversation.UpsertActiveOperation(current, conversation.SessionActiveOperation{
			Kind: conversation.OpKindBackgroundWorkflow, SubmissionID: sub.ID, ThreadID: threadID,
		})
		current.Status = conversation.SessionStatusWaitingBackground.String()
	})
	if err != nil {
		_ = a.AppState.FinalizeSubmission(sub.ID, domainsubmission.SubmissionStatusFailed.String())
		a.ClearSubmissionProcessingReactions(sub)
		return err
	}
	if !registrar.AddBackgroundWorkflowSteer(sessionKey, threadID, sub) {
		_ = a.AppState.FinalizeSubmission(sub.ID, domainsubmission.SubmissionStatusFailed.String())
		a.ClearSubmissionProcessingReactions(sub)
		_, _ = a.AppState.UpdateSession(sessionKey, func(current *conversation.Session) {
			conversation.RemoveActiveOperation(current, sub.ID, "")
			conversation.RefreshActiveStatus(current)
		})
		return fmt.Errorf("Claude background workflow stopped accepting steer input")
	}
	a.ReplyContinuation.RecordSubmissionSourceLinks(sub)
	a.MarkSubmissionRunningReactions(sub)
	return nil
}

func (s SubmissionQueueService) FailClaudeBackgroundWorkflow(sessionKey, threadID, submissionID, message string) {
	sub := s.Deps.AppState.Submission(submissionID)
	if sub == nil || sub.Finalized {
		return
	}
	steers := s.backgroundWorkflowSteerSubmissions(sessionKey, submissionID)
	for _, steer := range steers {
		_ = s.Deps.AppState.FinalizeSubmission(steer.ID, domainsubmission.SubmissionStatusFailed.String())
		s.Deps.ClearSubmissionProcessingReactions(steer)
	}
	if len(steers) > 0 {
		_, _ = s.Deps.AppState.UpdateSession(sessionKey, func(sess *conversation.Session) {
			for _, steer := range steers {
				conversation.RemoveActiveOperation(sess, steer.ID, "")
			}
		})
	}
	s.HandleSubmissionStartFailure(sessionKey, threadID, sub, fmt.Errorf("%s", strings.TrimSpace(message)), true)
}

func (s SubmissionQueueService) CancelClaudeBackgroundWorkflow(sessionKey, threadID, submissionID string) {
	a := s.Deps
	sub := a.AppState.Submission(submissionID)
	if sub == nil || sub.Finalized {
		return
	}
	if err := a.AppState.FinalizeSubmission(submissionID, domainsubmission.SubmissionStatusInterrupted.String()); err != nil {
		slog.Error("cancel Claude background workflow submission failed", "submission_id", submissionID, "error", err)
		return
	}
	a.ClearSubmissionProcessingReactions(sub)
	steers := s.backgroundWorkflowSteerSubmissions(sessionKey, submissionID)
	for _, steer := range steers {
		_ = a.AppState.FinalizeSubmission(steer.ID, domainsubmission.SubmissionStatusInterrupted.String())
		a.ClearSubmissionProcessingReactions(steer)
	}
	if _, err := a.AppState.UpdateSession(sessionKey, func(sess *conversation.Session) {
		conversation.RemoveActiveOperation(sess, submissionID, "")
		for _, steer := range steers {
			conversation.RemoveActiveOperation(sess, steer.ID, "")
		}
		conversation.RefreshActiveStatus(sess)
	}); err != nil {
		slog.Error("cancel Claude background workflow session failed", "submission_id", submissionID, "error", err)
	}
	a.RuntimeState.ClearPendingTurnBindingForSubmission(threadID, submissionID)
	a.RuntimeMaintenance.CleanupSubmissionRuntimeState(sub)
	if nextSessionKey := s.NextQueuedSessionKey(sessionKey); nextSessionKey != "" {
		runSessionAsync(a, nextSessionKey, func() {
			s.StartNextSubmissionAsync(nextSessionKey, "backgroundWorkflowCancelled")
		})
	}
}

func (s SubmissionQueueService) backgroundWorkflowSteerSubmissions(sessionKey, parentSubmissionID string) []*domainsubmission.Submission {
	sess := s.Deps.AppState.Session(sessionKey)
	if sess == nil {
		return nil
	}
	var steers []*domainsubmission.Submission
	for _, op := range sess.ActiveOperations {
		if op.Kind != conversation.OpKindBackgroundWorkflow || strings.TrimSpace(op.SubmissionID) == strings.TrimSpace(parentSubmissionID) {
			continue
		}
		if sub := s.Deps.AppState.Submission(op.SubmissionID); sub != nil && !sub.Finalized {
			steers = append(steers, sub)
		}
	}
	return steers
}

func (s SubmissionQueueService) startClaudeSubmissionAttempt(claude QueueClaudeClient, sessionKey string, sess *conversation.Session, sub *domainsubmission.Submission, claudeThreadID, prompt string, steer bool) (*conversation.Session, string, error) {
	a := s.Deps
	appState := a.AppState

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
	a.RuntimeState.MarkTurnStartedAt(turnID, time.Now())
	a.MarkSubmissionRunningReactions(sub)

	turnCtx, turnCancel := context.WithTimeout(a.context(), 20*time.Second)
	err = claude.StartTurn(turnCtx, sessionKey, claudeThreadID, turnID, prompt)
	turnCancel()
	if err != nil {
		return updatedSess, turnID, err
	}
	return updatedSess, turnID, nil
}

// startSteerSubmissionAttempt sends a steer message into the current
// conversation without creating a separate CLI turn.
func (s SubmissionQueueService) startSteerSubmissionAttempt(claude QueueClaudeClient, sessionKey string, sess *conversation.Session, sub *domainsubmission.Submission, claudeThreadID, prompt string) (*conversation.Session, string, error) {
	a := s.Deps
	appState := a.AppState

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
	a.MarkSubmissionRunningReactions(sub)

	turnCtx, turnCancel := context.WithTimeout(a.context(), 20*time.Second)
	err = claude.StartSteerTurn(turnCtx, sessionKey, claudeThreadID, turnID, prompt, sub.ID)
	turnCancel()
	if err != nil {
		return updatedSess, turnID, err
	}
	return updatedSess, turnID, nil
}

func (s SubmissionQueueService) rollbackClaudeSubmissionStartState(sessionKey string, sub *domainsubmission.Submission, turnID string, preserveLineage bool) (*conversation.Session, *domainsubmission.Submission, error) {
	a := s.Deps
	appState := a.AppState
	submissionID := ""
	if sub != nil {
		submissionID = strings.TrimSpace(sub.ID)
	}

	updatedSess, err := appState.UpdateSession(sessionKey, func(current *conversation.Session) {
		if current == nil {
			return
		}
		if submissionID != "" {
			conversation.RemoveActiveOperation(current, submissionID, turnID)
		}
		switch {
		case conversation.HasActiveOperations(current):
			current.Status = conversation.SessionStatusTurnStarting.String()
			for _, op := range current.ActiveOperations {
				if strings.TrimSpace(op.TurnID) != "" {
					current.Status = conversation.SessionStatusTurnInProgress.String()
					break
				}
			}
		case len(current.Queue) > 0 || len(current.StagedImages) > 0:
			if !preserveLineage {
				conversation.ClearThreadContext(current)
			}
			current.Status = conversation.SessionStatusQueued.String()
		default:
			if !preserveLineage {
				conversation.ClearThreadContext(current)
			}
			current.Status = conversation.SessionStatusIdle.String()
		}
	})
	if err != nil {
		return nil, nil, err
	}

	var refreshedSub *domainsubmission.Submission
	if submissionID != "" {
		if err := appState.UpdateSubmission(submissionID, func(current *domainsubmission.Submission) {
			if current == nil {
				return
			}
			current.ThreadID = ""
			current.TurnID = ""
			current.Status = conversation.SessionStatusQueued.String()
			current.Finalized = false
		}); err != nil {
			return updatedSess, nil, err
		}
		refreshedSub = appState.Submission(submissionID)
	}

	if strings.TrimSpace(turnID) != "" {
		if a.DeleteTurnArtifacts != nil {
			a.DeleteTurnArtifacts(turnID)
		}
		a.RuntimeState.ClearTurnBinding(turnID)
		if a.Items != nil {
			a.Items.ClearTurn(turnID)
		}
		a.TurnStream.DeleteTurnStream(turnID)
	}

	if !preserveLineage && (updatedSess == nil || !conversation.HasActiveOperations(updatedSess)) {
		a.LiveThread.ClearSessionLiveThread(sessionKey)
	}
	return updatedSess, refreshedSub, nil
}

func (s SubmissionQueueService) bindClaudeSubmissionStartState(sessionKey string, sess *conversation.Session, sub *domainsubmission.Submission, claudeThreadID, turnID string) (*conversation.Session, error) {
	a := s.Deps
	appState := a.AppState
	conversation.SetThreadContext(sess, sub.WorkspaceID, claudeThreadID, firstNonEmpty(strings.TrimSpace(sess.ActiveThreadName), "Claude"), firstNonEmpty(strings.TrimSpace(sess.ActiveThreadPreview), truncate(sub.InputText, 48)))
	updatedSess, err := appState.UpdateSession(sessionKey, func(current *conversation.Session) {
		if current == nil {
			return
		}
		conversation.SetThreadContext(current, sub.WorkspaceID, claudeThreadID, firstNonEmpty(strings.TrimSpace(current.ActiveThreadName), "Claude"), firstNonEmpty(strings.TrimSpace(current.ActiveThreadPreview), truncate(sub.InputText, 48)))
		op := conversation.SessionActiveOperation{
			Kind:         conversation.OpKindSubmission,
			SubmissionID: sub.ID,
			ThreadID:     claudeThreadID,
			TurnID:       turnID,
		}
		if conversation.FindActiveOperationBySubmission(current, sub.ID) != nil {
			conversation.UpsertActiveOperation(current, op)
		} else if conversation.HasActiveOperations(current) {
			conversation.PrependActiveOperation(current, op)
		} else {
			conversation.UpsertActiveOperation(current, op)
		}
		current.Status = conversation.SessionStatusTurnInProgress.String()
	})
	if err != nil {
		return nil, err
	}
	sub.ThreadID = claudeThreadID
	sub.TurnID = turnID
	sub.Status = domainsubmission.SubmissionStatusRunning.String()
	a.RuntimeState.BindTurnSubmission(claudeThreadID, turnID, sessionKey, sub.ID)
	if err := appState.MarkSubmissionRunning(sub.ID, claudeThreadID, turnID); err != nil {
		return nil, err
	}
	a.ReplyContinuation.RecordSubmissionSourceLinks(sub)
	recordLegacySessionRootTurnBinding(a.ReplyContinuation, updatedSess, sub, sessionKey, claudeThreadID, turnID)
	a.TurnStream.NoteTurnStarted(sessionKey, sub)
	if strings.TrimSpace(claudeThreadID) != "" {
		a.LiveThread.MarkSessionThreadLive(sessionKey, claudeThreadID)
	} else {
		a.LiveThread.ClearSessionLiveThread(sessionKey)
	}
	return updatedSess, nil
}

// truncate truncates s to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Keep the failed prompt at the head and do not schedule further work. A later
// input/queue retry re-attempts with the latest desired settings; /stop can
// still cancel it. No auto-retry, finalization or fresh-session fallback.
func (s SubmissionQueueService) deferClaudeModelConfig(sessionKey string, sub *domainsubmission.Submission, applyErr error, notify bool) error {
	appState := s.Deps.AppState
	if err := appState.UpdateSubmission(sub.ID, func(current *domainsubmission.Submission) {
		current.Status = domainsubmission.SubmissionStatusQueued.String()
		current.TurnID = ""
	}); err != nil {
		return err
	}
	_, err := appState.UpdateSession(sessionKey, func(current *conversation.Session) {
		conversation.RemoveActiveOperation(current, sub.ID, "")
		queue := []string{sub.ID}
		for _, id := range current.Queue {
			if id != sub.ID {
				queue = append(queue, id)
			}
		}
		current.Queue = queue
		current.ModelConfigError = applyErr.Error()
		if !conversation.HasActiveOperations(current) {
			current.Status = conversation.SessionStatusQueued.String()
		}
	})
	if err != nil {
		return err
	}
	s.Deps.MarkSubmissionQueuedReactions(sub)
	if notify {
		s.NotifySubmissionStartFailure(s.Deps.context(), sub, fmt.Errorf("%w；消息已保留在队首，请修正模型配置后发送新消息重试队列，或 /stop 取消", applyErr), false)
	}
	return applyErr
}

func canRetryFreshClaudeSession(claude QueueClaudeClient, sessionKey string) bool {
	if runtime, ok := claude.(interface{ CanRetryFreshSession(string) bool }); ok {
		return runtime.CanRetryFreshSession(sessionKey)
	}
	return true
}
