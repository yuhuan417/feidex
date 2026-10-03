package app

import (
	codexadapter "feidex/internal/adapter/backend/codex"
	"feidex/internal/application/presentation/usageview"
	"feidex/internal/codexrpc"
	domainsubmission "feidex/internal/domain/submission"
	domainturn "feidex/internal/domain/turn"
	"feidex/internal/runtime/turnbinding"
	"strings"
	"time"
)

func (s runtimeStateService) turnBindingTracker() *turnbinding.Tracker {
	if s.app == nil {
		return nil
	}
	trackers := s.app.Trackers()
	if trackers.turnBindings == nil {
		trackers.turnBindings = turnbinding.NewTracker(s.app.store)
	}
	return trackers.turnBindings
}

func (s runtimeStateService) notePendingTurnBinding(threadID, sessionKey, submissionID string) {
	tracker := s.turnBindingTracker()
	if tracker != nil {
		tracker.NotePendingTurnBinding(threadID, sessionKey, submissionID)
	}
}

func (s runtimeStateService) pendingSubmissionForThread(threadID string) (string, *domainsubmission.Submission) {
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

func (s runtimeStateService) boundSubmissionForTurn(turnID string) (string, *domainsubmission.Submission) {
	tracker := s.turnBindingTracker()
	if tracker == nil {
		return "", nil
	}
	return tracker.BoundSubmissionForTurn(turnID)
}

func (s runtimeStateService) BoundSubmissionForTurn(turnID string) (string, *domainsubmission.Submission) {
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
		tracker.RecordTurnTokenUsage(threadID, turnID, codexadapter.ThreadUsage(usage))
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
	binding, ok := tracker.TurnMetadata(turnID)
	usageLine, contextLine, elapsedLine = "", "", ""
	if ok && binding.HasLastUsage {
		usageLine = usageview.FormatTurnUsageLine(binding.LastUsage)
	}
	if ok {
		if binding.HasContextUsagePercent {
			contextLine = usageview.FormatContextUsedLine(binding.ContextUsagePercent)
		} else if usage, found := tracker.CurrentThreadUsage(binding.ThreadID); found && usage.ModelContextWindow != nil {
			contextLine = usageview.FormatContextLeftLine(usage.Last.InputTokens, *usage.ModelContextWindow)
		}
	}
	if ok && !binding.StartedAt.IsZero() && !completedAt.IsZero() {
		elapsedLine = usageview.FormatTurnElapsedLine(completedAt.Sub(binding.StartedAt))
	}
	return usageLine, contextLine, elapsedLine
}

func (s runtimeStateService) turnFinalFooterLines(turnID string, completedAt time.Time) []string {
	tracker := s.turnBindingTracker()
	if tracker == nil {
		return nil
	}
	_, contextLine, elapsedLine := s.turnFinalMetadata(turnID, completedAt)
	lines := make([]string, 0, 2)
	for _, line := range []string{contextLine, elapsedLine} {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func (s runtimeStateService) currentThreadUsage(threadID string) (domainturn.ThreadTokenUsage, bool) {
	tracker := s.turnBindingTracker()
	if tracker == nil {
		return domainturn.ThreadTokenUsage{}, false
	}
	usage, found := tracker.CurrentThreadUsage(threadID)
	return usage, found
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
func (s runtimeStateService) PendingSubmissionForThread(threadID string) (string, *domainsubmission.Submission) {
	return s.pendingSubmissionForThread(threadID)
}
func (s runtimeStateService) TurnFinalFooterLines(turnID string, completedAt time.Time) []string {
	return s.turnFinalFooterLines(turnID, completedAt)
}
func (s runtimeStateService) ClearTurnBinding(turnID string) { s.clearTurnBinding(turnID) }
func (s runtimeStateService) TurnBindingTracker() *turnbinding.Tracker {
	return s.turnBindingTracker()
}
func (s runtimeStateService) CurrentThreadUsage(threadID string) (domainturn.ThreadTokenUsage, bool) {
	return s.currentThreadUsage(threadID)
}
