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

func (a *App) turnBindingTracker() *turnBindingTracker {
	if a == nil {
		return nil
	}
	if a.turnBindings == nil {
		a.turnBindings = newTurnBindingTracker()
	}
	return a.turnBindings
}

func (a *App) notePendingTurnBinding(threadID, sessionKey, submissionID string) {
	if a == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	submissionID = strings.TrimSpace(submissionID)
	if threadID == "" {
		return
	}
	tracker := a.turnBindingTracker()
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

func (a *App) pendingSubmissionForThread(threadID string) (string, *state.Submission) {
	if a == nil {
		return "", nil
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", nil
	}
	appState := a.appState()
	tracker := a.turnBindingTracker()
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

func (a *App) clearPendingTurnBindingForSubmission(threadID, submissionID string) {
	if a == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	submissionID = strings.TrimSpace(submissionID)
	if threadID == "" || submissionID == "" {
		return
	}
	tracker := a.turnBindingTracker()
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

func (a *App) bindTurnSubmission(threadID, turnID, sessionKey, submissionID string) {
	if a == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker := a.turnBindingTracker()
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

func (a *App) rebindTurnThreadID(turnID, threadID string) {
	if a == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	threadID = strings.TrimSpace(threadID)
	if turnID == "" || threadID == "" {
		return
	}
	tracker := a.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	binding, ok := tracker.bindings[turnID]
	if !ok {
		return
	}
	binding.ThreadID = threadID
	tracker.bindings[turnID] = binding
}

func (a *App) boundSubmissionForTurn(turnID string) (string, *state.Submission) {
	if a == nil {
		return "", nil
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return "", nil
	}
	tracker := a.turnBindingTracker()
	tracker.mu.Lock()
	binding, ok := tracker.bindings[turnID]
	tracker.mu.Unlock()
	if !ok {
		return "", nil
	}
	sub := a.appState().submission(binding.SubmissionID)
	if sub == nil {
		return "", nil
	}
	return binding.SessionKey, sub
}

func (a *App) clearTurnBinding(turnID string) {
	if a == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker := a.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.bindings, turnID)
}

func (a *App) markTurnStartedAt(turnID string, startedAt time.Time) {
	if a == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker := a.turnBindingTracker()
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

func (a *App) recordTurnTokenUsage(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
	if a == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	tracker := a.turnBindingTracker()
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

func (a *App) recordTurnContextUsagePercent(turnID string, percentage float64) {
	if a == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	tracker := a.turnBindingTracker()
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

func (a *App) turnFinalMetadata(turnID string, completedAt time.Time) (usageLine, contextLine, elapsedLine string) {
	if a == nil {
		return "", "", ""
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return "", "", ""
	}
	tracker := a.turnBindingTracker()
	tracker.mu.Lock()
	binding, ok := tracker.bindings[turnID]
	tracker.mu.Unlock()
	if ok && binding.HasLastUsage {
		usageLine = formatTurnUsageLine(binding.LastUsage)
	}
	if ok {
		if binding.HasContextUsagePercent {
			contextLine = formatContextUsedLine(binding.ContextUsagePercent)
		} else if usage, found := a.currentThreadUsage(binding.ThreadID); found {
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

func (a *App) turnFinalFooterLines(turnID string, completedAt time.Time) []string {
	_, contextLine, elapsedLine := a.turnFinalMetadata(turnID, completedAt)
	lines := make([]string, 0, 2)
	for _, line := range []string{contextLine, elapsedLine} {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func (a *App) currentThreadUsage(threadID string) (codexrpc.ThreadTokenUsage, bool) {
	if a == nil {
		return codexrpc.ThreadTokenUsage{}, false
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return codexrpc.ThreadTokenUsage{}, false
	}
	tracker := a.turnBindingTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	usage, ok := tracker.threadUsage[threadID]
	return usage, ok
}
