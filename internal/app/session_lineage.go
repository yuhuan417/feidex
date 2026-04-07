package app

import (
	"strings"

	"feidex/internal/state"
)

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
