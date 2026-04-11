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

func (a *App) shouldStageInboundImages(msg *feishu.InboundMessage) bool {
	if msg == nil || strings.TrimSpace(msg.Text) != "" || len(msg.Attachments) == 0 {
		return false
	}
	for _, attachment := range msg.Attachments {
		if strings.TrimSpace(attachment.Kind) != "image" {
			return false
		}
	}
	return true
}

func (a *App) stageInboundImages(msg *feishu.InboundMessage) error {
	return a.stageInboundImagesForSession(msg, a.pendingInputSessionKey(msg))
}

func (a *App) stageInboundImagesForSession(msg *feishu.InboundMessage, sessionKey string) error {
	if msg == nil {
		return nil
	}
	appState := a.appState()
	sess := appState.session(sessionKey)
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
	attachments, err := a.resolveInboundAttachments(msg, sess.WorkspaceID, sessionKey)
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
	a.markMessagesQueuedReactions([]string{msg.MessageID})
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

func (a *App) discardPendingInputByMessageID(messageID string) bool {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false
	}
	appState := a.appState()
	for _, sess := range appState.sessions() {
		if sess == nil {
			continue
		}
		if discarded := discardStagedImageByMessageID(sess, messageID); discarded {
			if updateErr := appState.saveSession(sess); updateErr != nil {
				slog.Error("discard staged image session update failed", "session_key", sess.Key, "message_id", messageID, "error", updateErr)
				return false
			}
			a.markMessagesDiscardedReactions([]string{messageID})
			return true
		}
		for _, submissionID := range append([]string(nil), sess.Queue...) {
			sub := appState.submission(submissionID)
			if !submissionHasSourceMessage(sub, messageID) {
				continue
			}
			sess.Queue = removeString(sess.Queue, submissionID)
			if !sessionHasInFlightSubmission(sess) {
				if len(sess.Queue) == 0 && len(sess.StagedImages) == 0 {
					sess.Status = "idle"
				} else {
					sess.Status = "queued"
				}
			}
			if err := appState.saveSession(sess); err != nil {
				slog.Error("discard queued submission session update failed", "session_key", sess.Key, "submission_id", submissionID, "error", err)
				return false
			}
			if err := appState.updateSubmission(submissionID, func(value *state.Submission) {
				value.Status = "discarded"
				value.Finalized = true
			}); err != nil {
				slog.Error("discard queued submission update failed", "submission_id", submissionID, "error", err)
				return false
			}
			a.markMessagesDiscardedReactions(sourceMessageIDsForSubmission(sub))
			a.cleanupSubmissionRuntimeState(sub)
			return true
		}
	}
	return false
}

func (a *App) discardSessionPendingInputs(sessionKey string) int {
	appState := a.appState()
	sess := appState.session(sessionKey)
	if sess == nil {
		return 0
	}
	discarded := 0
	for _, image := range sess.StagedImages {
		a.markMessagesDiscardedReactions([]string{image.SourceMessageID})
		discarded++
	}
	queueIDs := append([]string(nil), sess.Queue...)
	for _, submissionID := range queueIDs {
		sub := appState.submission(submissionID)
		a.markMessagesDiscardedReactions(sourceMessageIDsForSubmission(sub))
		if err := appState.updateSubmission(submissionID, func(value *state.Submission) {
			value.Status = "discarded"
			value.Finalized = true
		}); err != nil {
			slog.Error("discard queued submission update failed", "submission_id", submissionID, "error", err)
			continue
		}
		discarded++
		a.cleanupSubmissionRuntimeState(sub)
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
	if !sessionHasInFlightSubmission(sess) && len(sess.Queue) == 0 && len(sess.StagedImages) == 0 {
		sess.Status = "idle"
	}
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

func (a *App) markSubmissionQueuedReactions(sub *state.Submission) {
	a.markMessagesQueuedReactions(sourceMessageIDsForSubmission(sub))
}

func (a *App) markSubmissionRunningReactions(sub *state.Submission) {
	ids := sourceMessageIDsForSubmission(sub)
	a.clearMessageProcessingReactions(ids)
	a.markMessagesTypingReactions(ids)
}

func (a *App) clearSubmissionProcessingReactions(sub *state.Submission) {
	a.clearMessageProcessingReactions(sourceMessageIDsForSubmission(sub))
}

func (a *App) markMessagesQueuedReactions(messageIDs []string) {
	a.forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		return a.feishu.AddReaction(ctx, messageID, queueReactionEmoji)
	})
}

func (a *App) markMessagesTypingReactions(messageIDs []string) {
	a.forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		return a.feishu.AddReaction(ctx, messageID, typingReactionEmoji)
	})
}

func (a *App) markMessagesDiscardedReactions(messageIDs []string) {
	a.clearMessageProcessingReactions(messageIDs)
	a.forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		return a.feishu.AddReaction(ctx, messageID, discardReactionEmoji)
	})
}

func (a *App) clearMessageProcessingReactions(messageIDs []string) {
	a.forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		if err := a.feishu.RemoveReaction(ctx, messageID, queueReactionEmoji); err != nil {
			return err
		}
		return a.feishu.RemoveReaction(ctx, messageID, typingReactionEmoji)
	})
}

func (a *App) forEachMessageID(messageIDs []string, fn func(context.Context, string) error) {
	if a == nil || a.feishu == nil || fn == nil {
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
