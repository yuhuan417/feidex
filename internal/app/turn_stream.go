package app

import (
	"context"
	"feidex/internal/adapter/feishu/turn"
	"feidex/internal/adapter/feishu/turnitem"
	domainsubmission "feidex/internal/domain/submission"
	"strings"

	appturnstream "feidex/internal/app/turnstream"
)

// Type aliases — exported types from the turnstream sub-package.
type (
	turnStreamTracker     = appturnstream.Tracker
	turnStream            = appturnstream.Stream
	turnStreamFlushResult = appturnstream.FlushResult
)

// turnStreamService wraps the turnstream sub-package service, delegating
// most methods while keeping *App-dependent logic local.
type turnStreamService struct {
	app     *App
	service appturnstream.Service
}

func newTurnStreamService(app *App) turnStreamService {
	return serviceFor(app, "turnStreamService", func() turnStreamService {
		return turnStreamService{
			app:     app,
			service: appturnstream.NewService(app),
		}
	})
}

func newTurnStreamTracker() *turnStreamTracker {
	return appturnstream.NewTracker()
}

// turnStreamTracker returns the tracker from the App's internal state.
func (s turnStreamService) turnStreamTracker() *turnStreamTracker {
	if s.app == nil {
		return nil
	}
	if s.app.trackers.turnStreams == nil {
		s.app.trackers.turnStreams = newTurnStreamTracker()
	}
	return s.app.trackers.turnStreams
}

// noteTurnStarted records that a turn has started, sending the submission
// started notice. This method is kept local because it calls
// maybeSendSubmissionStartedNotice which depends on *App.
func (s turnStreamService) noteTurnStarted(sessionKey string, sub *domainsubmission.Submission) {
	if sub == nil || strings.TrimSpace(sub.TurnID) == "" {
		return
	}
	s.maybeSendSubmissionStartedNotice(context.Background(), sub)
	tracker := s.turnStreamTracker()
	tracker.Mu.Lock()
	defer tracker.Mu.Unlock()
	s.service.EnsureStreamLocked(tracker, sessionKey, sub)
}

// maybeSendSubmissionStartedNotice sends the "turn started" notice if it
// hasn't been sent yet. This method is kept local because it depends on
// sendSubmissionStartedNotice which takes *App.
func (s turnStreamService) maybeSendSubmissionStartedNotice(ctx context.Context, sub *domainsubmission.Submission) {
	if s.app == nil || sub == nil || strings.TrimSpace(sub.ID) == "" {
		return
	}
	appState := s.app.State()
	shouldSend := false
	if err := appState.UpdateSubmission(sub.ID, func(current *domainsubmission.Submission) {
		if current == nil || !current.WaitedInQueue || current.StartNoticeSent {
			return
		}
		current.StartNoticeSent = true
		shouldSend = true
	}); err != nil || !shouldSend {
		return
	}
	updated := appState.Submission(sub.ID)
	if updated != nil {
		sub = updated
	}
	sendSubmissionStartedNotice(s.app, ctx, sub)
}

func (s turnStreamService) updatePendingPlan(turnID, plan string) {
	s.service.UpdatePendingPlan(turnID, plan)
}

func (s turnStreamService) recordTurnError(threadID, turnID, message string) {
	s.service.RecordTurnError(threadID, turnID, message)
}

func (s turnStreamService) completeTurnItem(ctx context.Context, threadID, turnID, itemID string, item map[string]any) {
	s.completeTurnItemPayload(ctx, threadID, turnID, itemID, turnitem.NewProtocolItemWithID(itemID, item))
}

func (s turnStreamService) completeTurnItemPayload(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
	s.service.CompleteTurnItem(ctx, threadID, turnID, itemID, item)
}

func (s turnStreamService) updateInFlightTurnItemPayload(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
	s.service.UpdateInFlightTurnItem(ctx, threadID, turnID, itemID, item)
}

func (s turnStreamService) flushTurnStream(ctx context.Context, threadID, turnID string) turnStreamFlushResult {
	return s.service.FlushTurnStream(ctx, threadID, turnID)
}

// Exported wrapper for sub-package interface satisfaction.
func (s turnStreamService) FlushTurnStream(ctx context.Context, threadID, turnID string) turnStreamFlushResult {
	return s.flushTurnStream(ctx, threadID, turnID)
}

func (s turnStreamService) turnStreamSawFinal(turnID string) bool {
	return s.service.StreamSawFinal(turnID)
}

func (s turnStreamService) deleteTurnStream(turnID string) {
	s.service.DeleteStream(turnID)
}

func (s turnStreamService) DeleteTurnStream(turnID string) { s.deleteTurnStream(turnID) }

func (s turnStreamService) markTurnStreamFinal(turnID string) {
	s.service.MarkStreamFinal(turnID)
}

func (s turnStreamService) discardWorkingCard(turnID string) {
	s.service.DiscardWorkingCard(turnID)
}

func (s turnStreamService) takeReasoningOnlyWorkingMessageID(turnID string) string {
	return s.service.TakeReasoningOnlyWorkingMessageID(turnID)
}

func (s turnStreamService) prepareTurnStreamQuietBoundary(turnID string) turn.QuietWorkingBoundary {
	return s.service.PrepareStreamQuietBoundary(turnID)
}

func (s turnStreamService) prepareTurnStreamQuietUpdatePayload(sessionKey string, sub *domainsubmission.Submission, threadID, itemID string, item turnitem.ProtocolItem, workspaceCwd string) turn.QuietWorkingCardOp {
	return s.service.PrepareStreamQuietUpdate(sessionKey, sub, threadID, itemID, item, workspaceCwd)
}

// Exported wrapper for sub-package interface satisfaction.
func (s turnStreamService) NoteTurnStarted(sessionKey string, sub *domainsubmission.Submission) {
	s.noteTurnStarted(sessionKey, sub)
}

func (s turnStreamService) commitTurnStreamQuietRender(turnID, messageID, body string) {
	s.service.CommitStreamQuietRender(turnID, messageID, body)
}

// isQuietBoundaryTurnPayload is a local helper that delegates to the turnstream package.
