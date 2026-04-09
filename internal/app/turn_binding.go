package app

import (
	"strings"

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
	sub := a.store.GetSubmission(binding.SubmissionID)
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
	sub := a.store.GetSubmission(binding.SubmissionID)
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
