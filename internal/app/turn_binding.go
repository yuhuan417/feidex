package app

import (
	"strings"
	"sync"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

type turnBindingTracker struct {
	mu          sync.Mutex
	bindings    map[string]turnBinding
	pending     map[string][]turnBinding
	threadUsage map[string]codexrpc.ThreadTokenUsage
	claudeUsage map[string]claudeThreadUsageSnapshot
}

func newTurnBindingTracker() *turnBindingTracker {
	return &turnBindingTracker{
		bindings:    map[string]turnBinding{},
		pending:     map[string][]turnBinding{},
		threadUsage: map[string]codexrpc.ThreadTokenUsage{},
		claudeUsage: map[string]claudeThreadUsageSnapshot{},
	}
}

func (s runtimeStateService) turnBindingTracker() *turnBindingTracker {
	if s.app == nil {
		return nil
	}
	if s.app.trackers.turnBindings == nil {
		s.app.trackers.turnBindings = newTurnBindingTracker()
	}
	return s.app.trackers.turnBindings
}

func (s runtimeStateService) notePendingTurnBinding(threadID, sessionKey, submissionID string) {
	if s.app == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	submissionID = strings.TrimSpace(submissionID)
	if threadID == "" {
		return
	}
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.pending == nil {
		tracker.pending = map[string][]turnBinding{}
	}
	for _, binding := range tracker.pending[threadID] {
		if strings.TrimSpace(binding.SubmissionID) == submissionID {
			return
		}
	}
	tracker.pending[threadID] = append(tracker.pending[threadID], turnBinding{
		ThreadID:     threadID,
		SessionKey:   strings.TrimSpace(sessionKey),
		SubmissionID: submissionID,
	})
}

func (s runtimeStateService) pendingSubmissionForThread(threadID string) (string, *state.Submission) {
	if s.app == nil {
		return "", nil
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", nil
	}
	appState := appState(s.app)
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	bindings := append([]turnBinding(nil), tracker.pending[threadID]...)
	next := make([]turnBinding, 0, len(bindings))
	var matched turnBinding
	found := false
	for _, binding := range bindings {
		sub := appState.submission(binding.SubmissionID)
		if sub == nil || sub.Finalized {
			continue
		}
		next = append(next, binding)
		if !found {
			matched = binding
			found = true
		}
	}
	if len(next) == 0 {
		delete(tracker.pending, threadID)
	} else {
		tracker.pending[threadID] = next
	}
	tracker.mu.Unlock()
	if !found {
		return "", nil
	}
	sub := appState.submission(matched.SubmissionID)
	if sub == nil {
		return "", nil
	}
	return matched.SessionKey, sub
}

func (s runtimeStateService) clearPendingTurnBindingForSubmission(threadID, submissionID string) {
	if s.app == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	submissionID = strings.TrimSpace(submissionID)
	if threadID == "" || submissionID == "" {
		return
	}
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	bindings := tracker.pending[threadID]
	if len(bindings) == 0 {
		return
	}
	next := make([]turnBinding, 0, len(bindings))
	for _, binding := range bindings {
		if strings.TrimSpace(binding.SubmissionID) == submissionID {
			continue
		}
		next = append(next, binding)
	}
	if len(next) == 0 {
		delete(tracker.pending, threadID)
		return
	}
	tracker.pending[threadID] = next
}

func (s runtimeStateService) bindTurnSubmission(threadID, turnID, sessionKey, submissionID string) {
	if s.app == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.bindings == nil {
		tracker.bindings = map[string]turnBinding{}
	}
	tracker.bindings[turnID] = turnBinding{
		ThreadID:     strings.TrimSpace(threadID),
		SessionKey:   strings.TrimSpace(sessionKey),
		SubmissionID: strings.TrimSpace(submissionID),
	}
}

func (s runtimeStateService) rebindTurnThreadID(turnID, threadID string) {
	if s.app == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	threadID = strings.TrimSpace(threadID)
	if turnID == "" || threadID == "" {
		return
	}
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	binding, ok := tracker.bindings[turnID]
	if !ok {
		return
	}
	binding.ThreadID = threadID
	tracker.bindings[turnID] = binding
}

func (s runtimeStateService) boundSubmissionForTurn(turnID string) (string, *state.Submission) {
	if s.app == nil {
		return "", nil
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return "", nil
	}
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	binding, ok := tracker.bindings[turnID]
	tracker.mu.Unlock()
	if !ok {
		return "", nil
	}
	sub := appState(s.app).submission(binding.SubmissionID)
	if sub == nil {
		return "", nil
	}
	return binding.SessionKey, sub
}

func (s runtimeStateService) clearTurnBinding(turnID string) {
	if s.app == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.bindings, turnID)
}

func (s runtimeStateService) markTurnStartedAt(turnID string, startedAt time.Time) {
	if s.app == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	binding, ok := tracker.bindings[turnID]
	if !ok {
		return
	}
	if binding.StartedAt.IsZero() {
		binding.StartedAt = startedAt
		tracker.bindings[turnID] = binding
	}
}

func (s runtimeStateService) recordTurnTokenUsage(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
	if s.app == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.threadUsage == nil {
		tracker.threadUsage = map[string]codexrpc.ThreadTokenUsage{}
	}
	if threadID != "" {
		tracker.threadUsage[threadID] = usage
	}
	if turnID == "" {
		return
	}
	binding, ok := tracker.bindings[turnID]
	if !ok {
		return
	}
	binding.LastUsage = usage.Last
	binding.HasLastUsage = true
	tracker.bindings[turnID] = binding
}

func (s runtimeStateService) recordTurnContextUsagePercent(turnID string, percentage float64) {
	if s.app == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	binding, ok := tracker.bindings[turnID]
	if !ok {
		return
	}
	binding.ContextUsagePercent = percentage
	binding.HasContextUsagePercent = true
	tracker.bindings[turnID] = binding
}

func (s runtimeStateService) turnFinalMetadata(turnID string, completedAt time.Time) (usageLine, contextLine, elapsedLine string) {
	if s.app == nil {
		return "", "", ""
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return "", "", ""
	}
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	binding, ok := tracker.bindings[turnID]
	tracker.mu.Unlock()
	if ok && binding.HasLastUsage {
		usageLine = formatTurnUsageLine(binding.LastUsage)
	}
	if ok {
		if binding.HasContextUsagePercent {
			contextLine = formatContextUsedLine(binding.ContextUsagePercent)
		} else if usage, found := s.currentThreadUsage(binding.ThreadID); found {
			if usage.ModelContextWindow != nil {
				contextLine = formatContextLeftLine(usage.Last.InputTokens, *usage.ModelContextWindow)
			}
		}
	}
	if ok && !binding.StartedAt.IsZero() && !completedAt.IsZero() {
		elapsedLine = formatTurnElapsedLine(completedAt.Sub(binding.StartedAt))
	}
	return usageLine, contextLine, elapsedLine
}

func (s runtimeStateService) turnFinalFooterLines(turnID string, completedAt time.Time) []string {
	_, contextLine, elapsedLine := s.turnFinalMetadata(turnID, completedAt)
	lines := make([]string, 0, 2)
	for _, line := range []string{contextLine, elapsedLine} {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func (s runtimeStateService) currentThreadUsage(threadID string) (codexrpc.ThreadTokenUsage, bool) {
	if s.app == nil {
		return codexrpc.ThreadTokenUsage{}, false
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return codexrpc.ThreadTokenUsage{}, false
	}
	tracker := s.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	usage, ok := tracker.threadUsage[threadID]
	return usage, ok
}
