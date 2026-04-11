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
	if threadID == "" {
		return
	}
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	if a.pendingTurns == nil {
		a.pendingTurns = map[string]turnBinding{}
	}
	a.pendingTurns[threadID] = turnBinding{
		ThreadID:     threadID,
		SessionKey:   strings.TrimSpace(sessionKey),
		SubmissionID: strings.TrimSpace(submissionID),
	}
}

func (a *App) pendingSubmissionForThread(threadID string) (string, *state.Submission) {
	if a == nil {
		return "", nil
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", nil
	}
	a.turnBindMu.Lock()
	binding, ok := a.pendingTurns[threadID]
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

func (a *App) clearPendingTurnBinding(threadID string) {
	if a == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	delete(a.pendingTurns, threadID)
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

func (a *App) noteTurnFirstFinal(turnID, text string) bool {
	if a == nil {
		return false
	}
	turnID = strings.TrimSpace(turnID)
	text = strings.TrimSpace(text)
	if turnID == "" || text == "" {
		return false
	}
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	binding, ok := a.turnBindings[turnID]
	if !ok {
		return false
	}
	if strings.TrimSpace(binding.FirstFinal) != "" {
		return false
	}
	binding.FirstFinal = text
	a.turnBindings[turnID] = binding
	return true
}

func (a *App) turnFinalText(turnID string) string {
	if a == nil {
		return ""
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return ""
	}
	a.turnBindMu.Lock()
	defer a.turnBindMu.Unlock()
	return strings.TrimSpace(a.turnBindings[turnID].FirstFinal)
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
		if usage, found := a.currentThreadUsage(binding.ThreadID); found {
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
