package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

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
	appState := appState(s.app)
	sess := appState.session(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   defaultWorkspaceID(s.app),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        "idle",
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = defaultWorkspaceID(s.app)
	}
	attachments, err := s.app.resolveInboundAttachments(msg, sess.WorkspaceID, sessionKey)
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
		sess.Status = "queued"
	}
	if err := appState.saveSession(sess); err != nil {
		return err
	}
	newPendingQueueService(s.app).markMessagesQueuedReactions([]string{msg.MessageID})
	return nil
}

func stagedImageAttachments(images []state.SessionStagedImage) []state.SubmissionAttachment {
	if len(images) == 0 {
		return nil
	}
	attachments := make([]state.SubmissionAttachment, 0, len(images))
	for _, image := range images {
		if strings.TrimSpace(image.LocalPath) == "" {
			continue
		}
		attachments = append(attachments, state.SubmissionAttachment{
			Kind:      "image",
			Name:      image.Name,
			LocalPath: image.LocalPath,
		})
	}
	return attachments
}

func stagedImageSourceMessageIDs(images []state.SessionStagedImage) []string {
	if len(images) == 0 {
		return nil
	}
	ids := make([]string, 0, len(images))
	for _, image := range images {
		ids = append(ids, image.SourceMessageID)
	}
	return uniqueStrings(ids)
}

func stagedImageRootMessageIDs(images []state.SessionStagedImage) []string {
	if len(images) == 0 {
		return nil
	}
	ids := make([]string, 0, len(images))
	for _, image := range images {
		rootID := firstNonEmpty(strings.TrimSpace(image.RootMessageID), strings.TrimSpace(image.SourceMessageID))
		if rootID == "" {
			continue
		}
		ids = append(ids, rootID)
	}
	return uniqueStrings(ids)
}

func (s pendingQueueService) discardPendingInputByMessageID(messageID string) bool {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false
	}
	appState := appState(s.app)
	for _, snapshot := range appState.sessions() {
		if snapshot == nil {
			continue
		}
		if newPendingQueueService(s.app).discardStagedImageFromSessionSnapshot(snapshot, messageID) {
			return true
		}
		for _, submissionID := range append([]string(nil), snapshot.Queue...) {
			sub := appState.submission(submissionID)
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
	appState := appState(s.app)
	discarded := false
	if _, err := appState.updateSession(snapshot.Key, func(current *state.Session) {
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
	appState := appState(s.app)
	discarded := false
	if _, err := appState.updateSession(snapshot.Key, func(current *state.Session) {
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
	if err := appState.updateSubmission(submissionID, func(value *state.Submission) {
		value.Status = "discarded"
		value.Finalized = true
	}); err != nil {
		slog.Error("discard queued submission update failed", "submission_id", submissionID, "error", err)
		return false
	}
	newPendingQueueService(s.app).markMessagesDiscardedReactions(sourceMessageIDsForSubmission(sub))
	newRuntimeMaintenanceService(s.app).cleanupSubmissionRuntimeState(sub)
	return true
}

func (s pendingQueueService) discardSessionPendingInputs(sessionKey string) int {
	appState := appState(s.app)
	sess := appState.session(sessionKey)
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
		sub := appState.submission(submissionID)
		newPendingQueueService(s.app).markMessagesDiscardedReactions(sourceMessageIDsForSubmission(sub))
		if err := appState.updateSubmission(submissionID, func(value *state.Submission) {
			value.Status = "discarded"
			value.Finalized = true
		}); err != nil {
			slog.Error("discard queued submission update failed", "submission_id", submissionID, "error", err)
			continue
		}
		discarded++
		newRuntimeMaintenanceService(s.app).cleanupSubmissionRuntimeState(sub)
	}
	sess.Queue = nil
	sess.StagedImages = nil
	if !sessionHasInFlightSubmission(sess) {
		sess.Status = "idle"
	}
	if err := appState.saveSession(sess); err != nil {
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
	if sub == nil {
		return false
	}
	for _, candidate := range sourceMessageIDsForSubmission(sub) {
		if candidate == messageID {
			return true
		}
	}
	return false
}

func sourceMessageIDsForSubmission(sub *state.Submission) []string {
	if sub == nil {
		return nil
	}
	ids := append([]string{}, sub.SourceMessageIDs...)
	if strings.TrimSpace(sub.TriggerMessageID) != "" {
		ids = append(ids, sub.TriggerMessageID)
	}
	return uniqueStrings(ids)
}

func (s pendingQueueService) markSubmissionQueuedReactions(sub *state.Submission) {
	newPendingQueueService(s.app).markMessagesQueuedReactions(sourceMessageIDsForSubmission(sub))
}

func (s pendingQueueService) markSubmissionRunningReactions(sub *state.Submission) {
	ids := sourceMessageIDsForSubmission(sub)
	newPendingQueueService(s.app).clearMessageProcessingReactions(ids)
	newPendingQueueService(s.app).markMessagesTypingReactions(ids)
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

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func removeString(values []string, target string) []string {
	target = strings.TrimSpace(target)
	if target == "" {
		return append([]string(nil), values...)
	}
	out := make([]string, 0, len(values))
	removed := false
	for _, value := range values {
		if !removed && strings.TrimSpace(value) == target {
			removed = true
			continue
		}
		out = append(out, value)
	}
	return out
}
