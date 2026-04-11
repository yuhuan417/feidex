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

// submissionWorkflow owns the submission queue and turn lifecycle flow.
type submissionWorkflow struct {
	app *App
}

func newSubmissionWorkflow(app *App) *submissionWorkflow {
	return &submissionWorkflow{app: app}
}

func (w *submissionWorkflow) enqueueSubmissionWithSessionKey(msg *feishu.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
	a := w.app
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   a.defaultWorkspaceID(),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        "idle",
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = a.defaultWorkspaceID()
	}
	inboundAttachments, err := a.resolveInboundAttachments(msg, sess.WorkspaceID, sessionKey)
	if err != nil {
		return err
	}
	bucketSessionKey := a.pendingInputSessionKey(msg)
	stagedImages := a.collectPendingStagedImages(sessionKey, bucketSessionKey)
	attachments := append(stagedImageAttachments(stagedImages), inboundAttachments...)
	sourceMessageIDs := uniqueStrings(append([]string{msg.MessageID}, stagedImageSourceMessageIDs(stagedImages)...))
	currentRootMessageID := firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))
	sourceRootMessageIDs := []string{currentRootMessageID}
	if !bindOnlyCurrentRoot {
		sourceRootMessageIDs = uniqueStrings(append(sourceRootMessageIDs, stagedImageRootMessageIDs(stagedImages)...))
	}
	if sessionHasInFlightSubmission(sess) {
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
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	logSessionState("submission enqueue session persisted", sessionKey, a.store.GetSession(sessionKey))
	sub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          sess.WorkspaceID,
		UserID:               msg.UserID,
		UserName:             msg.UserName,
		ChatID:               msg.ChatID,
		ChatName:             msg.ChatName,
		TriggerMessageID:     msg.MessageID,
		SourceMessageIDs:     sourceMessageIDs,
		SourceRootMessageIDs: sourceRootMessageIDs,
		InputText:            msg.Text,
		Attachments:          attachments,
		Status:               "queued",
	}
	id, err := a.store.CreateSubmission(sub)
	if err != nil {
		return err
	}
	a.recordInboundSubmissionSourceLink(msg.MessageID, sessionKey, id)
	if err := a.store.QueueSubmission(sessionKey, id); err != nil {
		return err
	}
	sub.ID = id
	if len(stagedImages) > 0 {
		if err := a.clearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
			return err
		}
	}
	slog.Debug("submission queued",
		"submission_id", id,
		"session_key", sessionKey,
		"active_thread_id", sess.ActiveThreadID,
		"active_turn_id", sess.ActiveTurnID,
	)
	logSessionState("submission queued session snapshot", sessionKey, a.store.GetSession(sessionKey))
	if !sessionHasInFlightSubmission(sess) {
		slog.Debug("submission starting immediately",
			"submission_id", id,
			"session_key", sessionKey,
		)
		return w.startNextSubmission(sessionKey)
	}
	a.markSubmissionQueuedReactions(sub)
	a.sendSubmissionQueuedNotice(context.Background(), sub)
	return nil
}

func (w *submissionWorkflow) startNextSubmission(sessionKey string) error {
	a := w.app
	sess := a.store.GetSession(sessionKey)
	logSessionState("startNextSubmission entry", sessionKey, sess)
	if sess == nil || sessionHasInFlightSubmission(sess) {
		slog.Debug("startNextSubmission skipped",
			"session_key", sessionKey,
			"has_session", sess != nil,
			"active_turn_id", func() string {
				if sess == nil {
					return ""
				}
				return sess.ActiveTurnID
			}(),
		)
		return nil
	}
	subID, err := a.store.DequeueSubmission(sessionKey)
	if err != nil || subID == "" {
		slog.Debug("startNextSubmission no queued item",
			"session_key", sessionKey,
			"error", err,
		)
		logSessionState("startNextSubmission empty-after-dequeue", sessionKey, a.store.GetSession(sessionKey))
		return err
	}
	logSessionState("startNextSubmission after dequeue", sessionKey, a.store.GetSession(sessionKey))
	sub := a.store.GetSubmission(subID)
	if sub == nil {
		slog.Warn("queued submission missing",
			"session_key", sessionKey,
			"submission_id", subID,
		)
		return nil
	}
	ws := config.FindWorkspace(a.cfg, sub.WorkspaceID)
	if ws == nil {
		slog.Error("workspace resolution failed",
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"default_workspace_id", a.defaultWorkspaceID(),
		)
		return fmt.Errorf("workspace %q not found", sub.WorkspaceID)
	}
	// Refresh the session after dequeue so we don't write a stale queue back.
	sess = a.store.GetSession(sessionKey)
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
	if threadID != "" && !a.sessionHasLiveThread(sessionKey, threadID) {
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
		threadParams := a.buildThreadStartParams(ws, sess, effectiveModel)
		var threadResp codexrpc.ThreadStartResult
		slog.Debug("thread start request",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"workspace_id", sub.WorkspaceID,
			"cwd", ws.Cwd,
			"model", effectiveModel,
		)
		threadCtx, threadCancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := a.codex.Call(threadCtx, "thread/start", threadParams, &threadResp)
		threadCancel()
		if err != nil {
			slog.Error("thread/start failed",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"workspace_id", sub.WorkspaceID,
				"cwd", ws.Cwd,
				"error", err,
			)
			logSessionState("startNextSubmission thread-start-failed", sessionKey, a.store.GetSession(sessionKey))
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
		a.markSessionThreadLive(sessionKey, threadID)
	}
	if threadID != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
		setSessionThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	}
	if threadID != "" {
		a.markSessionThreadLive(sessionKey, threadID)
	}
	sess.ActiveSubmissionID = sub.ID
	sess.Status = "turn_starting"
	sub.ThreadID = threadID
	sub.Status = "running"
	a.notePendingTurnBinding(threadID, sessionKey, sub.ID)
	if err := a.store.UpsertSession(sess); err != nil {
		a.clearPendingTurnBinding(threadID)
		return err
	}
	if err := a.store.UpdateSubmission(sub.ID, func(m *state.Submission) {
		m.ThreadID = threadID
		m.Status = "running"
	}); err != nil {
		a.clearPendingTurnBinding(threadID)
		return err
	}
	a.markSubmissionRunningReactions(sub)
	logSessionState("startNextSubmission session starting", sessionKey, a.store.GetSession(sessionKey))
	turnCtx, turnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	turnID, err := a.startSubmissionTurn(turnCtx, sessionKey, threadID, sub, ws.Cwd, effectiveApprovalPolicy, effectiveSandboxMode, effectiveServiceTier, effectiveModel, effectiveReasoningEffort)
	turnCancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("turn start timed out; waiting for delayed notification",
				"session_key", sessionKey,
				"submission_id", sub.ID,
				"thread_id", threadID,
				"workspace_id", sub.WorkspaceID,
			)
			logSessionState("startNextSubmission awaiting turn-start-notification", sessionKey, a.store.GetSession(sessionKey))
			return nil
		}
		a.clearPendingTurnBinding(threadID)
		a.clearSubmissionProcessingReactions(sub)
		slog.Error("turn start chain failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"workspace_id", sub.WorkspaceID,
			"error", err,
		)
		logSessionState("startNextSubmission turn-start-failed", sessionKey, a.store.GetSession(sessionKey))
		return err
	}
	slog.Debug("turn started",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	sess.ActiveSubmissionID = sub.ID
	sess.ActiveTurnID = turnID
	sess.Status = "turn_in_progress"
	a.bindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
	a.markTurnStartedAt(turnID, time.Now())
	a.clearPendingTurnBinding(threadID)
	sub.ThreadID = threadID
	sub.TurnID = turnID
	sub.Status = "running"
	if err := a.store.UpsertSession(sess); err != nil {
		return err
	}
	logSessionState("startNextSubmission session activated", sessionKey, a.store.GetSession(sessionKey))
	if err := a.store.UpdateSubmission(sub.ID, func(m *state.Submission) {
		m.ThreadID = threadID
		m.TurnID = turnID
		m.Status = "running"
	}); err != nil {
		return err
	}
	a.recordSubmissionSourceLinks(sub)
	a.recordRootTurnBinding(sess.RootMessageID, sessionKey, threadID, turnID)
	a.noteTurnStarted(sessionKey, sub)
	slog.Debug("startNextSubmission completed",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	return nil
}

func (w *submissionWorkflow) onTurnStartedNotification(threadID, turnID string) {
	a := w.app
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return
	}

	if sessionKey, sub := a.pendingSubmissionForThread(threadID); sub != nil {
		a.bindTurnSubmission(threadID, turnID, sessionKey, sub.ID)
		a.markTurnStartedAt(turnID, time.Now())
		a.clearPendingTurnBinding(threadID)

		sess := a.store.GetSession(sessionKey)
		if sess == nil {
			return
		}
		sess.ActiveSubmissionID = sub.ID
		sess.ActiveTurnID = turnID
		sess.Status = "turn_in_progress"
		setSessionThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
		if err := a.store.UpsertSession(sess); err != nil {
			return
		}
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
			s.ThreadID = threadID
			s.TurnID = turnID
			s.Status = "running"
		})
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
	for _, candidate := range a.store.AllSessions() {
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
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		slog.Warn("turn started notification missing session",
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
		)
		return
	}
	sub := a.store.GetSubmission(sess.ActiveSubmissionID)
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
	if err := a.store.UpsertSession(sess); err != nil {
		slog.Error("turn started notification session bind failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
			"error", err,
		)
		return
	}
	_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
		s.ThreadID = threadID
		s.TurnID = turnID
		s.Status = "running"
	})
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
	logSessionState("turn started notification session snapshot", sessionKey, a.store.GetSession(sessionKey))
}

func (w *submissionWorkflow) finishTurn(threadID, turnID, status string) {
	a := w.app
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
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
			s.Status = "completed"
			s.Finalized = true
		})
	case "interrupted":
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
			s.Status = "interrupted"
			s.Finalized = true
		})
	default:
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
			s.Status = "failed"
			s.Finalized = true
		})
	}
	sub = a.store.GetSubmission(sub.ID)
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
	sess := a.store.GetSession(sessionKey)
	if sess != nil {
		logSessionState("finishTurn before session clear", sessionKey, sess)
		sess.ActiveTurnID = ""
		sess.ActiveSubmissionID = ""
		sess.Status = "idle"
		_ = a.store.UpsertSession(sess)
		logSessionState("finishTurn after session clear", sessionKey, a.store.GetSession(sessionKey))
		slog.Debug("finishTurn scheduling next submission asynchronously",
			"session_key", sessionKey,
			"thread_id", sess.ActiveThreadID,
		)
		go w.startNextSubmissionAsync(sessionKey, "finishTurn")
	}
	a.cleanupSubmissionRuntimeState(sub)
}

func (w *submissionWorkflow) startNextSubmissionAsync(sessionKey, source string) {
	a := w.app
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	if err := w.startNextSubmission(sessionKey); err != nil {
		slog.Error("async startNextSubmission failed",
			"session_key", sessionKey,
			"source", source,
			"error", err,
		)
		logSessionState("async startNextSubmission failed snapshot", sessionKey, a.store.GetSession(sessionKey))
	}
}
