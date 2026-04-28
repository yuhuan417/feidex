package app

import (
	"time"

	"feidex/internal/app/turnbinding"
	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

func (s runtimeStateService) turnBindingTracker() *turnbinding.Tracker {
	if s.app == nil {
		return nil
	}
	if s.app.trackers.turnBindings == nil {
		s.app.trackers.turnBindings = turnbinding.NewTracker(s.app.store)
	}
	return s.app.trackers.turnBindings
}

func (s runtimeStateService) notePendingTurnBinding(threadID, sessionKey, submissionID string) {
	tracker := s.turnBindingTracker()
	if tracker != nil {
		tracker.NotePendingTurnBinding(threadID, sessionKey, submissionID)
	}
}

func (s runtimeStateService) pendingSubmissionForThread(threadID string) (string, *state.Submission) {
	tracker := s.turnBindingTracker()
	if tracker == nil {
		return "", nil
	}
	return tracker.PendingSubmissionForThread(threadID)
}

func (s runtimeStateService) clearPendingTurnBindingForSubmission(threadID, submissionID string) {
	tracker := s.turnBindingTracker()
	if tracker != nil {
		tracker.ClearPendingTurnBindingForSubmission(threadID, submissionID)
	}
}

func (s runtimeStateService) bindTurnSubmission(threadID, turnID, sessionKey, submissionID string) {
	tracker := s.turnBindingTracker()
	if tracker != nil {
		tracker.BindTurnSubmission(threadID, turnID, sessionKey, submissionID)
	}
}

func (s runtimeStateService) rebindTurnThreadID(turnID, threadID string) {
	tracker := s.turnBindingTracker()
	if tracker != nil {
		tracker.RebindTurnThreadID(turnID, threadID)
	}
}

func (s runtimeStateService) boundSubmissionForTurn(turnID string) (string, *state.Submission) {
	tracker := s.turnBindingTracker()
	if tracker == nil {
		return "", nil
	}
	return tracker.BoundSubmissionForTurn(turnID)
}

func (s runtimeStateService) BoundSubmissionForTurn(turnID string) (string, *state.Submission) {
	return s.boundSubmissionForTurn(turnID)
}

func (s runtimeStateService) clearTurnBinding(turnID string) {
	tracker := s.turnBindingTracker()
	if tracker != nil {
		tracker.ClearTurnBinding(turnID)
	}
}

func (s runtimeStateService) markTurnStartedAt(turnID string, startedAt time.Time) {
	tracker := s.turnBindingTracker()
	if tracker != nil {
		tracker.MarkTurnStartedAt(turnID, startedAt)
	}
}

func (s runtimeStateService) recordTurnTokenUsage(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
	tracker := s.turnBindingTracker()
	if tracker != nil {
		tracker.RecordTurnTokenUsage(threadID, turnID, usage)
	}
}

func (s runtimeStateService) recordTurnContextUsagePercent(turnID string, percentage float64) {
	tracker := s.turnBindingTracker()
	if tracker != nil {
		tracker.RecordTurnContextUsagePercent(turnID, percentage)
	}
}

func (s runtimeStateService) turnFinalMetadata(turnID string, completedAt time.Time) (usageLine, contextLine, elapsedLine string) {
	tracker := s.turnBindingTracker()
	if tracker == nil {
		return "", "", ""
	}
	return tracker.TurnFinalMetadata(turnID, completedAt, s.currentThreadUsage)
}

func (s runtimeStateService) turnFinalFooterLines(turnID string, completedAt time.Time) []string {
	tracker := s.turnBindingTracker()
	if tracker == nil {
		return nil
	}
	return tracker.TurnFinalFooterLines(turnID, completedAt, s.currentThreadUsage)
}

func (s runtimeStateService) currentThreadUsage(threadID string) (codexrpc.ThreadTokenUsage, bool) {
	tracker := s.turnBindingTracker()
	if tracker == nil {
		return codexrpc.ThreadTokenUsage{}, false
	}
	return tracker.CurrentThreadUsage(threadID)
}

// Exported wrappers so runtimeStateService directly satisfies sub-package
// provider interfaces (e.g. submission.QueueRuntimeStateProvider,
// turnlifecycle.RuntimeStateProvider, debugviewcmd.RuntimeStateProvider).

func (s runtimeStateService) NotePendingTurnBinding(threadID, sessionKey, submissionID string) {
	s.notePendingTurnBinding(threadID, sessionKey, submissionID)
}
func (s runtimeStateService) ClearPendingTurnBindingForSubmission(threadID, submissionID string) {
	s.clearPendingTurnBindingForSubmission(threadID, submissionID)
}
func (s runtimeStateService) BindTurnSubmission(threadID, turnID, sessionKey, submissionID string) {
	s.bindTurnSubmission(threadID, turnID, sessionKey, submissionID)
}
func (s runtimeStateService) MarkTurnStartedAt(turnID string, startedAt time.Time) {
	s.markTurnStartedAt(turnID, startedAt)
}
func (s runtimeStateService) PendingSubmissionForThread(threadID string) (string, *state.Submission) {
	return s.pendingSubmissionForThread(threadID)
}
func (s runtimeStateService) TurnFinalFooterLines(turnID string, completedAt time.Time) []string {
	return s.turnFinalFooterLines(turnID, completedAt)
}
func (s runtimeStateService) ClearTurnBinding(turnID string) { s.clearTurnBinding(turnID) }
func (s runtimeStateService) TurnBindingTracker() *turnbinding.Tracker {
	return s.turnBindingTracker()
}
func (s runtimeStateService) CurrentThreadUsage(threadID string) (codexrpc.ThreadTokenUsage, bool) {
	return s.currentThreadUsage(threadID)
}
