package app

import (
	"strings"

	"feidex/internal/state"
)

func (a *App) markSessionThreadLive(sessionKey, threadID string) {
	if a == nil || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return
	}
	a.liveThreadMu.Lock()
	defer a.liveThreadMu.Unlock()
	if a.liveThreads == nil {
		a.liveThreads = map[string]string{}
	}
	a.liveThreads[strings.TrimSpace(sessionKey)] = strings.TrimSpace(threadID)
}

func (a *App) sessionHasLiveThread(sessionKey, threadID string) bool {
	if a == nil || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return false
	}
	a.liveThreadMu.Lock()
	defer a.liveThreadMu.Unlock()
	return a.liveThreads[strings.TrimSpace(sessionKey)] == strings.TrimSpace(threadID)
}

func (a *App) clearSessionLiveThread(sessionKey string) {
	if a == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	a.liveThreadMu.Lock()
	defer a.liveThreadMu.Unlock()
	delete(a.liveThreads, strings.TrimSpace(sessionKey))
}

func sessionHasInFlightSubmission(sess *state.Session) bool {
	if sess == nil {
		return false
	}
	return strings.TrimSpace(sess.ActiveTurnID) != "" || strings.TrimSpace(sess.ActiveSubmissionID) != ""
}

func clearSessionThreadContext(sess *state.Session) {
	if sess == nil {
		return
	}
	sess.ActiveThreadID = ""
	sess.ActiveThreadWorkspaceID = ""
	sess.ActiveThreadName = ""
	sess.ActiveThreadPreview = ""
}

func setSessionThreadContext(sess *state.Session, workspaceID, threadID, name, preview string) {
	if sess == nil {
		return
	}
	sess.ActiveThreadID = strings.TrimSpace(threadID)
	sess.ActiveThreadWorkspaceID = strings.TrimSpace(workspaceID)
	sess.ActiveThreadName = strings.TrimSpace(name)
	sess.ActiveThreadPreview = strings.TrimSpace(preview)
}

func switchSessionWorkspace(sess *state.Session, workspaceID string) {
	if sess == nil {
		return
	}
	previousWorkspaceID := strings.TrimSpace(sess.WorkspaceID)
	sess.WorkspaceID = strings.TrimSpace(workspaceID)
	if sess.ActiveTurnID == "" && sess.ActiveSubmissionID == "" {
		clearSessionThreadContext(sess)
		return
	}
	if sess.ActiveThreadID != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
		sess.ActiveThreadWorkspaceID = previousWorkspaceID
	}
}

func sessionCanResumeThreadForSubmission(sess *state.Session, sub *state.Submission) bool {
	if sess == nil || sub == nil {
		return false
	}
	if strings.TrimSpace(sess.ActiveThreadID) == "" {
		return false
	}
	if strings.TrimSpace(sess.ActiveThreadWorkspaceID) == "" {
		return false
	}
	return strings.TrimSpace(sess.ActiveThreadWorkspaceID) == strings.TrimSpace(sub.WorkspaceID)
}
