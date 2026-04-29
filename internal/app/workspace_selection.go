package app

import (
	"strings"

	"feidex/internal/app/appcore"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func makeWorkspaceSelectionKey(a *App, chatType, chatID, userID string) string {
	return appcore.MakeWorkspaceSelectionKey(a, chatType, chatID, userID)
}

func workspaceSelectionSessionKeyForMessage(a *App, msg *feishu.InboundMessage) string {
	return appcore.MakeWorkspaceSelectionKeyForMessage(a, msg)
}

func workspaceSelectionSessionKeyForSession(a *App, sess *state.Session) string {
	return appcore.MakeWorkspaceSelectionKeyForSession(a, sess)
}

func resolveWorkspaceSelectionForMessage(a *App, msg *feishu.InboundMessage, fallback *state.Session) string {
	return appcore.ResolveWorkspaceSelectionForMessage(a, msg, fallback)
}

func resolveWorkspaceSelectionForSession(a *App, sess *state.Session) string {
	return appcore.ResolveWorkspaceSelectionForSession(a, sess)
}

func setWorkspaceSelectionForMessage(a *App, msg *feishu.InboundMessage, workspaceID string) error {
	return appcore.SetWorkspaceSelectionForMessage(a, msg, workspaceID)
}

func setWorkspaceSelectionForSession(a *App, sess *state.Session, workspaceID string) error {
	return appcore.SetWorkspaceSelectionForSession(a, sess, workspaceID)
}

func resolveThreadWorkspaceID(sess *state.Session, fallback string) string {
	if sess == nil {
		return strings.TrimSpace(fallback)
	}
	return firstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(fallback))
}

func resolveSubmissionWorkspaceID(a *App, msg *feishu.InboundMessage, sess *state.Session, bindOnlyCurrentRoot bool) string {
	if bindOnlyCurrentRoot {
		return firstNonEmpty(
			resolveThreadWorkspaceID(sess, ""),
			resolveWorkspaceSelectionForMessage(a, msg, sess),
			defaultWorkspaceID(a),
		)
	}
	return firstNonEmpty(
		resolveWorkspaceSelectionForMessage(a, msg, sess),
		strings.TrimSpace(func() string {
			if sess == nil {
				return ""
			}
			return sess.WorkspaceID
		}()),
		defaultWorkspaceID(a),
	)
}

func sessionCanRetargetWorkspaceSelection(sess *state.Session) bool {
	if sess == nil {
		return true
	}
	if sessionHasInFlightSubmission(sess) {
		return false
	}
	if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return false
	}
	if strings.TrimSpace(sess.ActiveThreadID) != "" || strings.TrimSpace(sess.ActiveThreadWorkspaceID) != "" {
		return false
	}
	return len(sess.BackendThreads) == 0
}
