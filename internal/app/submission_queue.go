package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (w *lifecycleCoordinator) enqueueSubmissionWithSessionKey(msg *feishu.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
	a := w.app
	appState := appState(a)
	sess := appState.session(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   defaultWorkspaceID(a),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        "idle",
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = defaultWorkspaceID(a)
	}
	if runtime := backendRuntime(a); runtime != nil {
		sess = runtime.reconcileCompletedTurnFromFinalOutput(a, sessionKey, sess)
	}
	if sess == nil {
		sess = appState.session(sessionKey)
	}
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   defaultWorkspaceID(a),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        "idle",
		}
	}
	inboundAttachments, err := resolveInboundAttachments(a, msg, sess.WorkspaceID, sessionKey)
	if err != nil {
		return err
	}
	bucketSessionKey := newReplyContinuationService(a).pendingInputSessionKey(msg)
	stagedImages := newReplyContinuationService(a).collectPendingStagedImages(sessionKey, bucketSessionKey)
	attachments := append(stagedImageAttachments(stagedImages), inboundAttachments...)
	skillResolution := newSkillsService(a).resolveSubmissionSkill(sessionKey, sess.WorkspaceID, msg.Text, attachments)
	if skillResolution.PendingReplacement != nil && strings.TrimSpace(skillResolution.InputText) == "" && len(attachments) == 0 {
		newSkillsService(a).setSessionPendingSkill(sessionKey, *skillResolution.PendingReplacement)
		if err := a.feishu.ReplyText(context.Background(), msg.MessageID, skillPendingConfirmationText(skillResolution.PendingReplacement.Name), replyInThreadEnabled(a, msg.ChatType)); err != nil {
			return err
		}
		return nil
	}
	sourceMessageIDs := uniqueStrings(append([]string{msg.MessageID}, stagedImageSourceMessageIDs(stagedImages)...))
	currentRootMessageID := firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))
	sourceRootMessageIDs := []string{currentRootMessageID}
	if !bindOnlyCurrentRoot {
		sourceRootMessageIDs = uniqueStrings(append(sourceRootMessageIDs, stagedImageRootMessageIDs(stagedImages)...))
	}
	mode := configuredSessionInflightMode(a)
	hasInFlight := sessionHasInFlightSubmission(sess)
	queueLenBefore := len(sess.Queue)
	shouldAttemptStart := !hasInFlight || sessionInflightAllowsAdditional(mode)
	willWaitInQueue := queueLenBefore > 0 || (hasInFlight && !sessionInflightAllowsAdditional(mode))
	if willWaitInQueue {
		sess.Status = "queued"
	}
	slog.Debug("submission enqueue begin",
		"session_key", sessionKey,
		"chat_id", msg.ChatID,
		"user_id", msg.UserID,
		"workspace_id", sess.WorkspaceID,
		"active_thread_id", sess.ActiveThreadID,
		"active_turn_id", sess.ActiveTurnID,
		"queue_len", len(sess.Queue),
	)
	if err := appState.saveSession(sess); err != nil {
		return err
	}
	logSessionState("submission enqueue session persisted", sessionKey, appState.session(sessionKey))
	sub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          sess.WorkspaceID,
		UserID:               msg.UserID,
		ChatID:               msg.ChatID,
		TriggerMessageID:     msg.MessageID,
		SourceMessageIDs:     sourceMessageIDs,
		SourceRootMessageIDs: sourceRootMessageIDs,
		InputText:            skillResolution.InputText,
		Skills:               skillResolution.Skills,
		Attachments:          attachments,
		Status:               "queued",
		WaitedInQueue:        willWaitInQueue,
	}
	id, err := appState.createSubmission(sub)
	if err != nil {
		return err
	}
	if err := appState.queueSubmission(sessionKey, id); err != nil {
		return err
	}
	sub.ID = id
	if len(stagedImages) > 0 {
		if err := newReplyContinuationService(a).clearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
			return err
		}
	}
	if skillResolution.ConsumePending {
		newSkillsService(a).clearSessionPendingSkill(sessionKey)
	}
	slog.Debug("submission queued",
		"submission_id", id,
		"session_key", sessionKey,
		"active_thread_id", sess.ActiveThreadID,
		"active_turn_id", sess.ActiveTurnID,
	)
	logSessionState("submission queued session snapshot", sessionKey, appState.session(sessionKey))
	if shouldAttemptStart {
		slog.Debug("submission starting immediately",
			"submission_id", id,
			"session_key", sessionKey,
		)
		if err := w.startNextSubmission(sessionKey); err != nil {
			return err
		}
		if !willWaitInQueue {
			return nil
		}
	}
	newPendingQueueService(a).markSubmissionQueuedReactions(sub)
	sendSubmissionQueuedNotice(a, context.Background(), sub)
	return nil
}

func (w *lifecycleCoordinator) startNextSubmission(sessionKey string) error {
	return w.startNextSubmissionWithFailureNotice(sessionKey, false)
}

func (w *lifecycleCoordinator) notifySubmissionStartFailure(ctx context.Context, sub *state.Submission, err error, willContinue bool) {
	a := w.app
	if a == nil || a.feishu == nil || sub == nil || err == nil {
		return
	}
	body := "任务启动失败: " + strings.TrimSpace(err.Error())
	if willContinue {
		body += "\n\n本条消息已跳过，正在继续处理后续排队消息。"
	} else {
		body += "\n\n本条消息未开始执行，可稍后重试。"
	}
	inThread := replyInThreadForSubmission(a, sub)
	if strings.TrimSpace(sub.TriggerMessageID) != "" {
		if replyErr := a.feishu.ReplyText(ctx, sub.TriggerMessageID, body, inThread); replyErr == nil {
			return
		}
	}
	if strings.TrimSpace(sub.ChatID) != "" {
		_ = a.feishu.SendText(ctx, sub.ChatID, body)
	}
}

func (w *lifecycleCoordinator) handleSubmissionStartFailure(sessionKey, threadID string, sub *state.Submission, err error, notifyFailure bool) {
	a := w.app
	appState := appState(a)
	dropThreadLineage := shouldDropCodexThreadLineageAfterStartFailure(a, err)
	if sub != nil {
		current := appState.submission(sub.ID)
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
		newRuntimeStateService(a).clearPendingTurnBindingForSubmission(threadID, sub.ID)
	}
	newPendingQueueService(a).clearSubmissionProcessingReactions(sub)
	if sub != nil {
		_ = appState.finalizeSubmission(sub.ID, "failed")
	}
	shouldStartNext := false
	clearedThreadLineage := false
	if sess, saveErr := appState.updateSession(sessionKey, func(sess *state.Session) {
		if sess == nil {
			return
		}
		if sub != nil {
			sessionRemoveActiveOperation(sess, sub.ID, "")
		}
		if dropThreadLineage && strings.TrimSpace(threadID) != "" && strings.TrimSpace(sess.ActiveThreadID) == strings.TrimSpace(threadID) {
			clearSessionThreadContext(sess)
			sessionClearBackendThread(sess, backendCodex)
			clearedThreadLineage = true
		}
		if !sessionHasActiveOperations(sess) {
			if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
				sess.Status = "queued"
			} else {
				sess.Status = "idle"
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
		shouldStartNext = sessionShouldStartNextSubmissionAsync(sess)
		newAutoRetryService(a).observeAutoRetryTerminal(sessionKey, threadID, "failed", sess, sub, "")
	}
	if clearedThreadLineage {
		clearSessionLiveThread(a, sessionKey)
	}
	if notifyFailure && sub != nil {
		w.notifySubmissionStartFailure(context.Background(), sub, err, shouldStartNext)
	}
	newRuntimeMaintenanceService(a).cleanupSubmissionRuntimeState(sub)
	if shouldStartNext {
		runAsync(a, func() {
			w.startNextSubmissionAsync(sessionKey, "turnStartFailed")
		})
	}
}

func shouldDropCodexThreadLineageAfterStartFailure(a *App, err error) bool {
	if a == nil || err == nil {
		return false
	}
	if runtime := backendRuntime(a); runtime != nil {
		return runtime.dropThreadLineageAfterStartFailure(a, err)
	}
	return false
}

func (w *lifecycleCoordinator) startNextSubmissionWithFailureNotice(sessionKey string, notifyFailure bool) error {
	a := w.app
	appState := appState(a)
	sess := appState.session(sessionKey)
	logSessionState("startNextSubmission entry", sessionKey, sess)
	if sess == nil {
		slog.Debug("startNextSubmission skipped", "session_key", sessionKey, "has_session", false)
		return nil
	}
	nextMode := configuredSessionInflightMode(a)
	if len(sess.Queue) > 0 {
		if nextSub := appState.submission(sess.Queue[0]); nextSub != nil {
			nextMode = configuredSessionInflightMode(a)
		}
	}
	if sessionHasInFlightSubmission(sess) && !sessionInflightAllowsAdditional(nextMode) {
		slog.Debug("startNextSubmission skipped",
			"session_key", sessionKey,
			"has_session", true,
			"active_turn_id", func() string {
				return sess.ActiveTurnID
			}(),
		)
		return nil
	}
	if runtime := backendRuntime(a); runtime != nil && runtime.deferQueuedSubmissionsDuringRecovery(a) {
		slog.Debug("startNextSubmission deferred",
			"session_key", sessionKey,
			"reason", "codex_runtime_recovering",
		)
		return nil
	}
	subID, err := appState.dequeueSubmission(sessionKey)
	if err != nil || subID == "" {
		slog.Debug("startNextSubmission no queued item",
			"session_key", sessionKey,
			"error", err,
		)
		if err == nil {
			updatedSess, updateErr := appState.updateSession(sessionKey, func(current *state.Session) {
				if current == nil {
					return
				}
				sessionRefreshPendingStatus(current)
			})
			if updateErr != nil {
				return updateErr
			}
			logSessionState("startNextSubmission empty-after-dequeue", sessionKey, updatedSess)
		} else {
			logSessionState("startNextSubmission empty-after-dequeue", sessionKey, appState.session(sessionKey))
		}
		return err
	}
	logSessionState("startNextSubmission after dequeue", sessionKey, appState.session(sessionKey))
	sub := appState.submission(subID)
	if sub == nil {
		slog.Warn("queued submission missing",
			"session_key", sessionKey,
			"submission_id", subID,
		)
		updatedSess, updateErr := appState.updateSession(sessionKey, func(current *state.Session) {
			if current == nil {
				return
			}
			sessionRefreshPendingStatus(current)
		})
		if updateErr != nil {
			return updateErr
		}
		logSessionState("startNextSubmission after missing queued item", sessionKey, updatedSess)
		if updatedSess != nil && !sessionHasInFlightSubmission(updatedSess) && len(updatedSess.Queue) > 0 {
			return w.startNextSubmissionWithFailureNotice(sessionKey, notifyFailure)
		}
		return nil
	}
	ws := config.FindWorkspace(a.cfg, sub.WorkspaceID)
	if ws == nil {
		slog.Error("workspace resolution failed",
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"default_workspace_id", defaultWorkspaceID(a),
		)
		return fmt.Errorf("workspace %q not found", sub.WorkspaceID)
	}
	sess = appState.session(sessionKey)
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
	return conversationBackend(a).startQueuedSubmission(w, sessionKey, sess, sub, ws, notifyFailure)
}

func (w *lifecycleCoordinator) startNextCodexSubmissionWithFailureNotice(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	a := w.app
	appState := appState(a)
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	if !sessionCanResumeThreadForSubmission(sess, sub) {
		if strings.TrimSpace(sess.ActiveThreadID) != "" {
			slog.Debug("dropping session thread lineage for new submission",
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
	if threadID != "" && !sessionHasLiveThread(a, sessionKey, threadID) {
		slog.Warn("dropping non-live session thread before submission",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"workspace_id", sub.WorkspaceID,
		)
		threadID = ""
		clearSessionThreadContext(sess)
	}
	effectiveModel := configuredGlobalModel(a.cfg)
	effectiveReasoningEffort := configuredGlobalReasoningEffort(a.cfg)
	effectiveApprovalPolicy := effectiveThreadApprovalPolicy(sess, ws)
	effectiveSandboxMode := effectiveThreadSandboxMode(sess, ws)
	effectiveServiceTier := effectiveThreadServiceTier(sess)
	if threadID == "" {
		client, err := requireCodexClient(a)
		if err != nil {
			w.handleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
			logSessionState("startNextSubmission thread-client-missing", sessionKey, appState.session(sessionKey))
			return err
		}
		threadParams := buildThreadStartParams(a, ws, sess, effectiveModel)
		var threadResp codexrpc.ThreadStartResult
		slog.Debug("thread start request",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"cwd", ws.Cwd,
			"model", effectiveModel,
		)
		threadCtx, threadCancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = client.Call(threadCtx, "thread/start", threadParams, &threadResp)
		threadCancel()
		if err != nil {
			w.handleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
			slog.Error("thread/start failed",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"workspace_id", sub.WorkspaceID,
				"cwd", ws.Cwd,
				"error", err,
			)
			logSessionState("startNextSubmission thread-start-failed", sessionKey, appState.session(sessionKey))
			return err
		}
		threadID = threadResp.Thread.ID
		slog.Debug("thread started",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"model", effectiveModel,
		)
		setSessionThreadContext(sess, sub.WorkspaceID, threadID, threadResp.Thread.Name, threadResp.Thread.Preview)
		markSessionThreadLive(a, sessionKey, threadID)
	}
	if threadID != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
		setSessionThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	}
	if threadID != "" {
		markSessionThreadLive(a, sessionKey, threadID)
	}
	sessionUpsertActiveOperation(sess, state.SessionActiveOperation{
		Kind:         sessionOpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
	})
	sess.Status = "turn_starting"
	sub.ThreadID = threadID
	sub.Status = "running"
	newRuntimeStateService(a).notePendingTurnBinding(threadID, sessionKey, sub.ID)
	if err := appState.saveSession(sess); err != nil {
		newRuntimeStateService(a).clearPendingTurnBindingForSubmission(threadID, sub.ID)
		return err
	}
	if err := appState.markSubmissionRunning(sub.ID, threadID, ""); err != nil {
		newRuntimeStateService(a).clearPendingTurnBindingForSubmission(threadID, sub.ID)
		return err
	}
	newPendingQueueService(a).markSubmissionRunningReactions(sub)
	logSessionState("startNextSubmission session starting", sessionKey, appState.session(sessionKey))
	turnCtx, turnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	turnID := ""
	var err error
	if isReviewSubmission(sub) {
		turnID, err = startSubmissionReview(a, turnCtx, threadID, sub)
	} else {
		turnID, err = startSubmissionTurn(a, turnCtx, sessionKey, threadID, sub, ws.Cwd, effectiveApprovalPolicy, effectiveSandboxMode, effectiveServiceTier, effectiveModel, effectiveReasoningEffort)
	}
	turnCancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("turn start timed out; waiting for delayed notification",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"thread_id", threadID,
				"workspace_id", sub.WorkspaceID,
			)
			logSessionState("startNextSubmission awaiting turn-start-notification", sessionKey, appState.session(sessionKey))
			return nil
		}
		w.handleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
		slog.Error("turn start chain failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"workspace_id", sub.WorkspaceID,
			"error", err,
		)
		logSessionState("startNextSubmission turn-start-failed", sessionKey, appState.session(sessionKey))
		return err
	}
	slog.Debug("turn started",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	sessionUpsertActiveOperation(sess, state.SessionActiveOperation{
		Kind:         sessionOpKindSubmission,
		SubmissionID: sub.ID,
		ThreadID:     threadID,
		TurnID:       turnID,
	})
	sess.Status = "turn_in_progress"
	newRuntimeStateService(a).bindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	newRuntimeStateService(a).markTurnStartedAt(turnID, time.Now())
	newRuntimeStateService(a).clearPendingTurnBindingForSubmission(threadID, sub.ID)
	sub.ThreadID = threadID
	sub.TurnID = turnID
	sub.Status = "running"
	if err := appState.saveSession(sess); err != nil {
		return err
	}
	logSessionState("startNextSubmission session activated", sessionKey, appState.session(sessionKey))
	if err := appState.markSubmissionRunning(sub.ID, threadID, turnID); err != nil {
		return err
	}
	newReplyContinuationService(a).recordSubmissionSourceLinks(sub)
	newReplyContinuationService(a).recordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
	newTurnStreamService(a).noteTurnStarted(sessionKey, sub)
	slog.Debug("startNextSubmission completed",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	return nil
}

func (w *lifecycleCoordinator) startNextSubmissionAsync(sessionKey, source string) {
	a := w.app
	appState := appState(a)
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	if !sessionShouldStartNextSubmissionAsync(appState.session(sessionKey)) {
		return
	}
	if err := w.startNextSubmissionWithFailureNotice(sessionKey, true); err != nil {
		slog.Error("async startNextSubmission failed",
			"session_key", sessionKey,
			"source", source,
			"error", err,
		)
		logSessionState("async startNextSubmission failed snapshot", sessionKey, appState.session(sessionKey))
	}
}
