package app

import (
	"strings"
	"sync"
)

type submissionStartTracker struct {
	mu       sync.Mutex
	sessions map[string]submissionStartState
}

type submissionStartState struct {
	running bool
	pending bool
}

func (t *submissionStartTracker) tryBegin(sessionKey string) bool {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessions == nil {
		t.sessions = map[string]submissionStartState{}
	}
	state := t.sessions[sessionKey]
	if state.running {
		state.pending = true
		t.sessions[sessionKey] = state
		return false
	}
	state.running = true
	state.pending = false
	t.sessions[sessionKey] = state
	return true
}

func (t *submissionStartTracker) finish(sessionKey string) bool {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessions == nil {
		return false
	}
	state, ok := t.sessions[sessionKey]
	if !ok {
		return false
	}
	rerun := state.pending
	delete(t.sessions, sessionKey)
	return rerun
}

func tryBeginSessionSubmissionStart(a *App, sessionKey string) bool {
	if a == nil {
		return false
	}
	return a.trackers.submissionStarts.tryBegin(sessionKey)
}

func finishSessionSubmissionStart(a *App, sessionKey string) bool {
	if a == nil {
		return false
	}
	return a.trackers.submissionStarts.finish(sessionKey)
}
