package app

import (
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

func (a *App) notePendingTurnBinding(threadID, sessionKey, submissionID string) {
	if a == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	submissionID = strings.TrimSpace(submissionID)
	if threadID == "" {
		return
	}
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	if a.pendingTurns == nil {
		a.pendingTurns = map[string][]turnBinding{}
	}
	for _, binding := range a.pendingTurns[threadID] {
		if strings.TrimSpace(binding.SubmissionID) == submissionID {
			return
		}
	}
	a.pendingTurns[threadID] = append(a.pendingTurns[threadID], turnBinding{
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
	a.turnBindMu.Lock()
	bindings := append([]turnBinding(nil), a.pendingTurns[threadID]...)
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
		delete(a.pendingTurns, threadID)
	} else {
		a.pendingTurns[threadID] = next
	}
	a.turnBindMu.Unlock()
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
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	bindings := a.pendingTurns[threadID]
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
		delete(a.pendingTurns, threadID)
		return
	}
	a.pendingTurns[threadID] = next
}

func (a *App) bindTurnSubmission(threadID, turnID, sessionKey, submissionID string) {
	if a == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	if a.turnBindings == nil {
		a.turnBindings = map[string]turnBinding{}
	}
	a.turnBindings[turnID] = turnBinding{
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
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	binding, ok := a.turnBindings[turnID]
	if !ok {
		return
	}
	binding.ThreadID = threadID
	a.turnBindings[turnID] = binding
}

func (a *App) boundSubmissionForTurn(turnID string) (string, *state.Submission) {
	if a == nil {
		return "", nil
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return "", nil
	}
	a.turnBindMu.Lock()
	binding, ok := a.turnBindings[turnID]
	a.turnBindMu.Unlock()
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
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	delete(a.turnBindings, turnID)
}

func (a *App) markTurnStartedAt(turnID string, startedAt time.Time) {
	if a == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	binding, ok := a.turnBindings[turnID]
	if !ok {
		return
	}
	if binding.StartedAt.IsZero() {
		binding.StartedAt = startedAt
		a.turnBindings[turnID] = binding
	}
}

func (a *App) recordTurnTokenUsage(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
	if a == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	if a.threadUsage == nil {
		a.threadUsage = map[string]codexrpc.ThreadTokenUsage{}
	}
	if threadID != "" {
		a.threadUsage[threadID] = usage
	}
	if turnID == "" {
		return
	}
	binding, ok := a.turnBindings[turnID]
	if !ok {
		return
	}
	binding.LastUsage = usage.Last
	binding.HasLastUsage = true
	a.turnBindings[turnID] = binding
}

func (a *App) recordTurnContextUsagePercent(turnID string, percentage float64) {
	if a == nil {
		return
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	binding, ok := a.turnBindings[turnID]
	if !ok {
		return
	}
	binding.ContextUsagePercent = percentage
	binding.HasContextUsagePercent = true
	a.turnBindings[turnID] = binding
}

func (a *App) turnFinalMetadata(turnID string, completedAt time.Time) (usageLine, contextLine, elapsedLine string) {
	if a == nil {
		return "", "", ""
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return "", "", ""
	}
	a.turnBindMu.Lock()
	binding, ok := a.turnBindings[turnID]
	a.turnBindMu.Unlock()
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
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	usage, ok := a.threadUsage[threadID]
	return usage, ok
}
