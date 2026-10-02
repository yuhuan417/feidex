package submission

import (
	"context"
	"feidex/internal/application"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"log/slog"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Explicit pending input dependencies
// ---------------------------------------------------------------------------

// PendingDependencies contains the repository, cleanup and presentation ports for pending input.
type PendingDependencies struct {
	Context            func() context.Context
	State              PendingQueueStateProvider
	Maintenance        PendingQueueRuntimeMaintenanceProvider
	DefaultWorkspaceID func() string
	AddReaction        func(context.Context, string, string) error
	RemoveReaction     func(context.Context, string, string) error
	LogSessionState    func(string, string, *conversation.Session)
}

// PendingQueueStateProvider narrows app state access.
type PendingQueueStateProvider interface {
	Session(key string) *conversation.Session
	Sessions() []*conversation.Session
	Submission(id string) *domainsubmission.Submission
	SaveSession(sess *conversation.Session) error
	UpdateSession(key string, mutate func(*conversation.Session)) (*conversation.Session, error)
	UpdateSubmission(id string, mutate func(*domainsubmission.Submission)) error
}

// PendingQueueRuntimeMaintenanceProvider narrows runtime maintenance.
type PendingQueueRuntimeMaintenanceProvider interface {
	CleanupSubmissionRuntimeState(sub *domainsubmission.Submission)
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// PendingQueueService manages pending queue operations: staging images,
// discarding items, and managing Feishu reactions.
type PendingQueueService struct {
	Deps PendingDependencies
}

// NewPendingQueueService creates a new PendingQueueService.
func NewPendingQueueService(deps PendingDependencies) PendingQueueService {
	return PendingQueueService{Deps: deps}
}

// Reaction emoji constants.
const (
	QueueReactionEmoji   = "OneSecond"
	TypingReactionEmoji  = "THINKING"
	DiscardReactionEmoji = "ThumbsDown"
)

// ShouldStageInboundImages returns true if the message should be staged as
// pending files to be included in the next submission.  Any attachment-only
// message without text (images, STL, PDF, etc.) is eligible.
func (s PendingQueueService) ShouldStageInboundImages(msg *application.InboundMessage) bool {
	if msg == nil || msg.ExpandedMergeForward || strings.TrimSpace(msg.Text) != "" || len(msg.Attachments) == 0 {
		return false
	}
	return true
}

// StageInboundImagesForSession stages images from an inbound message into
// the session's staged images list.
func (s PendingQueueService) StageInboundImagesForSession(msg *application.InboundMessage, sessionKey string, resolveAttachments func(msg *application.InboundMessage, workspaceID, sessionKey string) ([]domainsubmission.SubmissionAttachment, error)) error {
	if msg == nil {
		return nil
	}
	a := s.Deps
	appState := a.State
	sess := appState.Session(sessionKey)
	if sess == nil {
		sess = &conversation.Session{
			Key:           sessionKey,
			WorkspaceID:   a.DefaultWorkspaceID(),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        conversation.SessionStatusIdle.String(),
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = a.DefaultWorkspaceID()
	}
	attachments, err := resolveAttachments(msg, sess.WorkspaceID, sessionKey)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, attachment := range attachments {
		sess.StagedImages = append(sess.StagedImages, conversation.SessionStagedImage{
			SourceMessageID: msg.MessageID,
			RootMessageID:   firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID)),
			Name:            attachment.Name,
			LocalPath:       attachment.LocalPath,
			CreatedAt:       now,
		})
	}
	if conversation.HasInFlightSubmission(sess) || len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		sess.Status = conversation.SessionStatusQueued.String()
	}
	if err := appState.SaveSession(sess); err != nil {
		return err
	}
	s.markMessagesQueuedReactions([]string{msg.MessageID})
	return nil
}

// DiscardPendingInputByMessageID discards a pending input (staged image or
// queued submission) that originated from the given message ID.
func (s PendingQueueService) DiscardPendingInputByMessageID(messageID string) bool {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false
	}
	a := s.Deps
	appState := a.State
	for _, snapshot := range appState.Sessions() {
		if snapshot == nil {
			continue
		}
		if s.DiscardStagedImageFromSessionSnapshot(snapshot, messageID) {
			return true
		}
		for _, submissionID := range append([]string(nil), snapshot.Queue...) {
			sub := appState.Submission(submissionID)
			if !HasSourceMessage(sub, messageID) {
				continue
			}
			if !s.DiscardQueuedSubmissionFromSessionSnapshot(snapshot, submissionID, sub) {
				return false
			}
			return true
		}
	}
	return false
}

// DiscardStagedImageFromSessionSnapshot discards a staged image from a session
// snapshot by source message ID.
func (s PendingQueueService) DiscardStagedImageFromSessionSnapshot(snapshot *conversation.Session, messageID string) bool {
	a := s.Deps
	if snapshot == nil || strings.TrimSpace(snapshot.Key) == "" {
		return false
	}
	appState := a.State
	discarded := false
	if _, err := appState.UpdateSession(snapshot.Key, func(current *conversation.Session) {
		if current == nil {
			return
		}
		discarded = DiscardStagedImageByMessageID(current, messageID)
	}); err != nil {
		slog.Error("discard staged image session update failed", "session_key", snapshot.Key, "message_id", messageID, "error", err)
		return false
	}
	if !discarded {
		return false
	}
	s.markMessagesDiscardedReactions([]string{messageID})
	return true
}

// DiscardQueuedSubmissionFromSessionSnapshot discards a queued submission
// from a session snapshot.
func (s PendingQueueService) DiscardQueuedSubmissionFromSessionSnapshot(snapshot *conversation.Session, submissionID string, sub *domainsubmission.Submission) bool {
	a := s.Deps
	if snapshot == nil || strings.TrimSpace(snapshot.Key) == "" || strings.TrimSpace(submissionID) == "" {
		return false
	}
	appState := a.State
	discarded := false
	if _, err := appState.UpdateSession(snapshot.Key, func(current *conversation.Session) {
		if current == nil {
			return
		}
		nextQueue := RemoveString(current.Queue, submissionID)
		if len(nextQueue) == len(current.Queue) {
			return
		}
		current.Queue = nextQueue
		conversation.RefreshPendingStatus(current)
		discarded = true
	}); err != nil {
		slog.Error("discard queued submission session update failed", "session_key", snapshot.Key, "submission_id", submissionID, "error", err)
		return false
	}
	if !discarded {
		return false
	}
	if err := appState.UpdateSubmission(submissionID, func(value *domainsubmission.Submission) {
		value.Status = domainsubmission.SubmissionStatusDiscarded.String()
		value.Finalized = true
	}); err != nil {
		slog.Error("discard queued submission update failed", "submission_id", submissionID, "error", err)
		return false
	}
	s.markMessagesDiscardedReactions(SourceMessageIDs(sub))
	a.Maintenance.CleanupSubmissionRuntimeState(sub)
	return true
}

// DiscardSessionPendingInputs discards all pending inputs (staged images and
// queued submissions) for a session.
func (s PendingQueueService) DiscardSessionPendingInputs(sessionKey string) int {
	a := s.Deps
	appState := a.State
	sess := appState.Session(sessionKey)
	if sess == nil {
		return 0
	}
	discarded := 0
	for _, image := range sess.StagedImages {
		s.markMessagesDiscardedReactions([]string{image.SourceMessageID})
		discarded++
	}
	queueIDs := append([]string(nil), sess.Queue...)
	for _, submissionID := range queueIDs {
		sub := appState.Submission(submissionID)
		s.markMessagesDiscardedReactions(SourceMessageIDs(sub))
		if err := appState.UpdateSubmission(submissionID, func(value *domainsubmission.Submission) {
			value.Status = domainsubmission.SubmissionStatusDiscarded.String()
			value.Finalized = true
		}); err != nil {
			slog.Error("discard queued submission update failed", "submission_id", submissionID, "error", err)
			continue
		}
		discarded++
		a.Maintenance.CleanupSubmissionRuntimeState(sub)
	}
	sess.Queue = nil
	sess.StagedImages = nil
	if !conversation.HasInFlightSubmission(sess) {
		sess.Status = conversation.SessionStatusIdle.String()
	}
	if err := appState.SaveSession(sess); err != nil {
		slog.Error("discard session pending inputs failed", "session_key", sessionKey, "error", err)
	}
	return discarded
}

// MarkSubmissionQueuedReactions marks all source messages of a submission
// with the queue reaction emoji.
func (s PendingQueueService) MarkSubmissionQueuedReactions(sub *domainsubmission.Submission) {
	s.markMessagesQueuedReactions(SourceMessageIDs(sub))
}

// MarkSubmissionRunningReactions clears queue reactions and marks source
// messages with the typing reaction emoji.
func (s PendingQueueService) MarkSubmissionRunningReactions(sub *domainsubmission.Submission) {
	ids := SourceMessageIDs(sub)
	s.clearMessageProcessingReactions(ids)
	s.markMessagesTypingReactions(ids)
}

// ClearSubmissionProcessingReactions clears all processing reactions for a
// submission's source messages.
func (s PendingQueueService) ClearSubmissionProcessingReactions(sub *domainsubmission.Submission) {
	s.clearMessageProcessingReactions(SourceMessageIDs(sub))
}

// MarkMessagesQueuedReactions marks arbitrary messages with the queue emoji.
func (s PendingQueueService) MarkMessagesQueuedReactions(messageIDs []string) {
	s.markMessagesQueuedReactions(messageIDs)
}

// MarkMessagesTypingReactions marks arbitrary messages with the typing emoji.
func (s PendingQueueService) MarkMessagesTypingReactions(messageIDs []string) {
	s.markMessagesTypingReactions(messageIDs)
}

// MarkMessagesDiscardedReactions marks arbitrary messages as discarded.
func (s PendingQueueService) MarkMessagesDiscardedReactions(messageIDs []string) {
	s.markMessagesDiscardedReactions(messageIDs)
}

// ClearMessageProcessingReactions clears queue and typing reactions from
// arbitrary messages.
func (s PendingQueueService) ClearMessageProcessingReactions(messageIDs []string) {
	s.clearMessageProcessingReactions(messageIDs)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s PendingQueueService) markMessagesQueuedReactions(messageIDs []string) {
	s.forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		return s.Deps.AddReaction(ctx, messageID, QueueReactionEmoji)
	})
}

func (s PendingQueueService) markMessagesTypingReactions(messageIDs []string) {
	s.forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		return s.Deps.AddReaction(ctx, messageID, TypingReactionEmoji)
	})
}

func (s PendingQueueService) markMessagesDiscardedReactions(messageIDs []string) {
	s.clearMessageProcessingReactions(messageIDs)
	s.forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		return s.Deps.AddReaction(ctx, messageID, DiscardReactionEmoji)
	})
}

func (s PendingQueueService) clearMessageProcessingReactions(messageIDs []string) {
	s.forEachMessageID(messageIDs, func(ctx context.Context, messageID string) error {
		if err := s.Deps.RemoveReaction(ctx, messageID, QueueReactionEmoji); err != nil {
			return err
		}
		return s.Deps.RemoveReaction(ctx, messageID, TypingReactionEmoji)
	})
}

func (s PendingQueueService) forEachMessageID(messageIDs []string, fn func(context.Context, string) error) {
	if fn == nil {
		return
	}
	ids := UniqueStrings(messageIDs)
	for _, messageID := range ids {
		ctx, cancel := context.WithTimeout(s.Deps.context(), 5*time.Second)
		err := fn(ctx, messageID)
		cancel()
		if err != nil {
			slog.Warn("feishu reaction operation failed", "message_id", messageID, "error", err)
		}
	}
}

// DiscardStagedImageByMessageID removes the first staged image with the given
// source message ID from a session.
func DiscardStagedImageByMessageID(sess *conversation.Session, messageID string) bool {
	if sess == nil || len(sess.StagedImages) == 0 {
		return false
	}
	next := make([]conversation.SessionStagedImage, 0, len(sess.StagedImages))
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
	conversation.RefreshPendingStatus(sess)
	return true
}

func (d PendingDependencies) context() context.Context {
	if d.Context != nil {
		return d.Context()
	}
	return context.Background()
}
