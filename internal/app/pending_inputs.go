package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/app/submission"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type pendingQueueService struct {
	app *App
}

func newPendingQueueService(app *App) pendingQueueService {
	return pendingQueueService{app: app}
}

const (
	queueReactionEmoji   = "OneSecond"
	typingReactionEmoji  = "THINKING"
	discardReactionEmoji = "ThumbsDown"
)

func (s pendingQueueService) shouldStageInboundImages(msg *feishu.InboundMessage) bool {
	if msg == nil || msg.ExpandedMergeForward || strings.TrimSpace(msg.Text) != "" || len(msg.Attachments) == 0 {
		return false
	}
	for _, attachment := range msg.Attachments {
		if strings.TrimSpace(attachment.Kind) != "image" {
			return false
		}
	}
	return true
}

func (s pendingQueueService) stageInboundImagesForSession(msg *feishu.InboundMessage, sessionKey string) error {
	if msg == nil {
		return nil
	}
	appState := s.app.State()
	sess := appState.Session(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   defaultWorkspaceID(s.app),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        state.SessionStatusIdle.String(),
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = defaultWorkspaceID(s.app)
	}
	attachments, err := resolveInboundAttachments(s.app, msg, sess.WorkspaceID, sessionKey)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, attachment := range attachments {
		sess.StagedImages = append(sess.StagedImages, state.SessionStagedImage{
			SourceMessageID: msg.MessageID,
			RootMessageID:   firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID)),
			Name:            attachment.Name,
			LocalPath:       attachment.LocalPath,
			CreatedAt:       now,
		})
	}
	if sessionHasInFlightSubmission(sess) || len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		sess.Status = state.SessionStatusQueued.String()
	}
	if err := appState.SaveSession(sess); err != nil {
		return err
	}
	newPendingQueueService(s.app).markMessagesQueuedReactions([]string{msg.MessageID})
	return nil
}

// stagedImageAttachments, stagedImageSourceMessageIDs, and
// stagedImageRootMessageIDs are re-exported in replycontinuation_bindings.go.

func (s pendingQueueService) discardPendingInputByMessageID(messageID string) bool {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false
	}
	appState := s.app.State()
	for _, snapshot := range appState.Sessions() {
		if snapshot == nil {
			continue
		}
		if newPendingQueueService(s.app).discardStagedImageFromSessionSnapshot(snapshot, messageID) {
			return true
		}
		for _, submissionID := range append([]string(nil), snapshot.Queue...) {
			sub := appState.Submission(submissionID)
			if !submissionHasSourceMessage(sub, messageID) {
				continue
			}
			if !newPendingQueueService(s.app).discardQueuedSubmissionFromSessionSnapshot(snapshot, submissionID, sub) {
				return false
			}
			return true
		}
	}
	return false
}

func (s pendingQueueService) discardStagedImageFromSessionSnapshot(snapshot *state.Session, messageID string) bool {
	if s.app == nil || snapshot == nil || strings.TrimSpace(snapshot.Key) == "" {
		return false
	}
	appState := s.app.State()
	discarded := false
	if _, err := appState.UpdateSession(snapshot.Key, func(current *state.Session) {
		if current == nil {
			return
		}
		discarded = discardStagedImageByMessageID(current, messageID)
	}); err != nil {
		slog.Error("discard staged image session update failed", "session_key", snapshot.Key, "message_id", messageID, "error", err)
		return false
	}
	if !discarded {
		return false
	}
	newPendingQueueService(s.app).markMessagesDiscardedReactions([]string{messageID})
	return true
}

func (s pendingQueueService) discardQueuedSubmissionFromSessionSnapshot(snapshot *state.Session, submissionID string, sub *state.Submission) bool {
	if s.app == nil || snapshot == nil || strings.TrimSpace(snapshot.Key) == "" || strings.TrimSpace(submissionID) == "" {
		return false
	}
	appState := s.app.State()
	discarded := false
	if _, err := appState.UpdateSession(snapshot.Key, func(current *state.Session) {
		if current == nil {
			return
		}
		nextQueue := removeString(current.Queue, submissionID)
		if len(nextQueue) == len(current.Queue) {
			return
		}
		current.Queue = nextQueue
		sessionRefreshPendingStatus(current)
		discarded = true
	}); err != nil {
		slog.Error("discard queued submission session update failed", "session_key", snapshot.Key, "submission_id", submissionID, "error", err)
		return false
	}
	if !discarded {
		return false
	}
	if err := appState.UpdateSubmission(submissionID, func(value *state.Submission) {
		value.Status = state.SubmissionStatusDiscarded.String()
		value.Finalized = true
	}); err != nil {
		slog.Error("discard queued submission update failed", "submission_id", submissionID, "error", err)
		return false
	}
	newPendingQueueService(s.app).markMessagesDiscardedReactions(sourceMessageIDsForSubmission(sub))
	newRuntimeMaintenanceService(s.app).CleanupSubmissionRuntimeState(sub)
	return true
}

// Exported wrapper for sub-package interface satisfaction.
func (s pendingQueueService) DiscardSessionPendingInputs(sessionKey string) int {
	return s.discardSessionPendingInputs(sessionKey)
}

func (s pendingQueueService) discardSessionPendingInputs(sessionKey string) int {
	appState := s.app.State()
	sess := appState.Session(sessionKey)
	if sess == nil {
		return 0
	}
	discarded := 0
	for _, image := range sess.StagedImages {
		newPendingQueueService(s.app).markMessagesDiscardedReactions([]string{image.SourceMessageID})
		discarded++
	}
	queueIDs := append([]string(nil), sess.Queue...)
	for _, submissionID := range queueIDs {
		sub := appState.Submission(submissionID)
		newPendingQueueService(s.app).markMessagesDiscardedReactions(sourceMessageIDsForSubmission(sub))
		if err := appState.UpdateSubmission(submissionID, func(value *state.Submission) {
			value.Status = state.SubmissionStatusDiscarded.String()
			value.Finalized = true
		}); err != nil {
			slog.Error("discard queued submission update failed", "submission_id", submissionID, "error", err)
			continue
		}
		discarded++
		newRuntimeMaintenanceService(s.app).CleanupSubmissionRuntimeState(sub)
	}
	sess.Queue = nil
	sess.StagedImages = nil
	if !sessionHasInFlightSubmission(sess) {
		sess.Status = state.SessionStatusIdle.String()
	}
	if err := appState.SaveSession(sess); err != nil {
		slog.Error("discard session pending inputs failed", "session_key", sessionKey, "error", err)
	}
	return discarded
}

func discardStagedImageByMessageID(sess *state.Session, messageID string) bool {
	if sess == nil || len(sess.StagedImages) == 0 {
		return false
	}
	next := make([]state.SessionStagedImage, 0, len(sess.StagedImages))
	discarded := false
	for _, image := range sess.StagedImages {
		if !discarded && strings.TrimSpace(image.SourceMessageID) == messageID {
			discarded = true
			continue
		}
		next = append(next, image)
	}
	if !discarded {
		return false
	}
	sess.StagedImages = next
	sessionRefreshPendingStatus(sess)
	return true
}

func submissionHasSourceMessage(sub *state.Submission, messageID string) bool {
	return submission.HasSourceMessage(sub, messageID)
}

// sourceMessageIDsForSubmission is re-exported in
// replycontinuation_bindings.go.

func (s pendingQueueService) markSubmissionQueuedReactions(sub *state.Submission) {
	newPendingQueueService(s.app).markMessagesQueuedReactions(sourceMessageIDsForSubmission(sub))
}

func (s pendingQueueService) markSubmissionRunningReactions(sub *state.Submission) {
	ids := sourceMessageIDsForSubmission(sub)
	newPendingQueueService(s.app).clearMessageProcessingReactions(ids)
	newPendingQueueService(s.app).markMessagesTypingReactions(ids)
}

// Exported wrapper for sub-package interface satisfaction.
func (s pendingQueueService) ClearSubmissionProcessingReactions(sub *state.Submission) {
	s.clearSubmissionProcessingReactions(sub)
}

func (s pendingQueueService) clearSubmissionProcessingReactions(sub *state.Submission) {
	newPendingQueueService(s.app).clearMessageProcessingReactions(sourceMessageIDsForSubmission(sub))
}

func (s pendingQueueService) markMessagesQueuedReactions(messageIDs []string) {
	newPendingQueueService(s.app).forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		return s.app.feishu.AddReaction(ctx, messageID, queueReactionEmoji)
	})
}

func (s pendingQueueService) markMessagesTypingReactions(messageIDs []string) {
	newPendingQueueService(s.app).forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		return s.app.feishu.AddReaction(ctx, messageID, typingReactionEmoji)
	})
}

func (s pendingQueueService) markMessagesDiscardedReactions(messageIDs []string) {
	newPendingQueueService(s.app).clearMessageProcessingReactions(messageIDs)
	newPendingQueueService(s.app).forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		return s.app.feishu.AddReaction(ctx, messageID, discardReactionEmoji)
	})
}

func (s pendingQueueService) clearMessageProcessingReactions(messageIDs []string) {
	newPendingQueueService(s.app).forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		if err := s.app.feishu.RemoveReaction(ctx, messageID, queueReactionEmoji); err != nil {
			return err
		}
		return s.app.feishu.RemoveReaction(ctx, messageID, typingReactionEmoji)
	})
}

func (s pendingQueueService) forEachMessageID(messageIDs []string, fn func(context.Context, string) error) {
	if s.app == nil || s.app.feishu == nil || fn == nil {
		return
	}
	ids := uniqueStrings(messageIDs)
	for _, messageID := range ids {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := fn(ctx, messageID)
		cancel()
		if err != nil {
			slog.Warn("feishu reaction operation failed", "message_id", messageID, "error", err)
		}
	}
}

var uniqueStrings = submission.UniqueStrings

var removeString = submission.RemoveString
