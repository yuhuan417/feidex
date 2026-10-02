package app

import (
	"feidex/internal/app/submission"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/feishu"
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
	return serviceFor(app, "pendingQueueService", func() pendingQueueService {
		return pendingQueueService{
			app:   app,
			inner: newPendingQueueServiceFromApp(app),
		}
	})
}

func (s pendingQueueService) shouldStageInboundImages(msg *feishu.InboundMessage) bool {
	return s.inner.ShouldStageInboundImages(msg)
}

func (s pendingQueueService) stageInboundImagesForSession(msg *feishu.InboundMessage, sessionKey string) error {
	return s.inner.StageInboundImagesForSession(msg, sessionKey, func(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]domainsubmission.SubmissionAttachment, error) {
		return resolveInboundAttachments(s.app, msg, workspaceID, sessionKey)
	})
}

func (s pendingQueueService) discardPendingInputByMessageID(messageID string) bool {
	return s.inner.DiscardPendingInputByMessageID(messageID)
}

// Exported wrapper for sub-package interface satisfaction.
func (s pendingQueueService) DiscardSessionPendingInputs(sessionKey string) int {
	return s.discardSessionPendingInputs(sessionKey)
}

func (s pendingQueueService) discardSessionPendingInputs(sessionKey string) int {
	return s.inner.DiscardSessionPendingInputs(sessionKey)
}

func (s pendingQueueService) markSubmissionQueuedReactions(sub *domainsubmission.Submission) {
	s.inner.MarkSubmissionQueuedReactions(sub)
}

func (s pendingQueueService) markSubmissionRunningReactions(sub *domainsubmission.Submission) {
	s.inner.MarkSubmissionRunningReactions(sub)
}

// Exported wrapper for sub-package interface satisfaction.
func (s pendingQueueService) ClearSubmissionProcessingReactions(sub *domainsubmission.Submission) {
	s.clearSubmissionProcessingReactions(sub)
}

func (s pendingQueueService) clearSubmissionProcessingReactions(sub *domainsubmission.Submission) {
	s.inner.ClearSubmissionProcessingReactions(sub)
}

func (s pendingQueueService) markMessagesQueuedReactions(messageIDs []string) {
	s.inner.MarkMessagesQueuedReactions(messageIDs)
}

func (s pendingQueueService) clearMessageProcessingReactions(messageIDs []string) {
	s.inner.ClearMessageProcessingReactions(messageIDs)
}

var (
	uniqueStrings = submission.UniqueStrings
)
