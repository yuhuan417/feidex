package app

import (
	"feidex/internal/app/submission"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const (
	queueReactionEmoji   = submission.QueueReactionEmoji
	typingReactionEmoji  = submission.TypingReactionEmoji
	discardReactionEmoji = submission.DiscardReactionEmoji
)

// pendingQueueService preserves the root app method surface used by event
// routing, bindings, and tests while delegating the real implementation to
// submission.PendingQueueService.
type pendingQueueService struct {
	app   *App
	inner submission.PendingQueueService
}

func newPendingQueueService(app *App) pendingQueueService {
	return pendingQueueService{
		app:   app,
		inner: newPendingQueueServiceFromApp(app),
	}
}

func (s pendingQueueService) shouldStageInboundImages(msg *feishu.InboundMessage) bool {
	return s.inner.ShouldStageInboundImages(msg)
}

func (s pendingQueueService) stageInboundImagesForSession(msg *feishu.InboundMessage, sessionKey string) error {
	return s.inner.StageInboundImagesForSession(msg, sessionKey, func(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]state.SubmissionAttachment, error) {
		return resolveInboundAttachments(s.app, msg, workspaceID, sessionKey)
	})
}

func (s pendingQueueService) discardPendingInputByMessageID(messageID string) bool {
	return s.inner.DiscardPendingInputByMessageID(messageID)
}

func (s pendingQueueService) discardStagedImageFromSessionSnapshot(snapshot *state.Session, messageID string) bool {
	return s.inner.DiscardStagedImageFromSessionSnapshot(snapshot, messageID)
}

func (s pendingQueueService) discardQueuedSubmissionFromSessionSnapshot(snapshot *state.Session, submissionID string, sub *state.Submission) bool {
	return s.inner.DiscardQueuedSubmissionFromSessionSnapshot(snapshot, submissionID, sub)
}

// Exported wrapper for sub-package interface satisfaction.
func (s pendingQueueService) DiscardSessionPendingInputs(sessionKey string) int {
	return s.discardSessionPendingInputs(sessionKey)
}

func (s pendingQueueService) discardSessionPendingInputs(sessionKey string) int {
	return s.inner.DiscardSessionPendingInputs(sessionKey)
}

func (s pendingQueueService) markSubmissionQueuedReactions(sub *state.Submission) {
	s.inner.MarkSubmissionQueuedReactions(sub)
}

func (s pendingQueueService) markSubmissionRunningReactions(sub *state.Submission) {
	s.inner.MarkSubmissionRunningReactions(sub)
}

// Exported wrapper for sub-package interface satisfaction.
func (s pendingQueueService) ClearSubmissionProcessingReactions(sub *state.Submission) {
	s.clearSubmissionProcessingReactions(sub)
}

func (s pendingQueueService) clearSubmissionProcessingReactions(sub *state.Submission) {
	s.inner.ClearSubmissionProcessingReactions(sub)
}

func (s pendingQueueService) markMessagesQueuedReactions(messageIDs []string) {
	s.inner.MarkMessagesQueuedReactions(messageIDs)
}

func (s pendingQueueService) markMessagesTypingReactions(messageIDs []string) {
	s.inner.MarkMessagesTypingReactions(messageIDs)
}

func (s pendingQueueService) markMessagesDiscardedReactions(messageIDs []string) {
	s.inner.MarkMessagesDiscardedReactions(messageIDs)
}

func (s pendingQueueService) clearMessageProcessingReactions(messageIDs []string) {
	s.inner.ClearMessageProcessingReactions(messageIDs)
}

var (
	uniqueStrings                 = submission.UniqueStrings
	removeString                  = submission.RemoveString
	discardStagedImageByMessageID = submission.DiscardStagedImageByMessageID
	submissionHasSourceMessage    = submission.HasSourceMessage
)
