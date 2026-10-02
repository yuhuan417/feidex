package app

import (
	frontendruntime "feidex/internal/runtime"
	"strings"

	"feidex/internal/app/sessionctx"
	"feidex/internal/config"
	"feidex/internal/state"
)

func getAppLiveThreadTracker(a *App) *frontendruntime.LiveThreads {
	if a == nil {
		return nil
	}
	if a.liveThreads == nil {
		a.liveThreads = frontendruntime.NewLiveThreads()
	}
	return a.liveThreads
}

func markSessionThreadLive(a *App, sessionKey, threadID string) {
	if a == nil || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return
	}
	tracker := getAppLiveThreadTracker(a)
	tracker.Mark(sessionKey, threadID)
	if sess := a.State().Session(sessionKey); sess != nil {
		chatID := strings.TrimSpace(sess.ChatID)
		if chatID == "" {
			_, _, chatID, _, _ = parseSessionKey(sess.Key)
		}
		if sessionMatchesGroupChat(a, sess, chatID) {
			scheduleGroupAnnouncementStatusRefresh(a, chatID, "thread_live")
		}
	}
}

func sessionHasLiveThread(a *App, sessionKey, threadID string) bool {
	if a == nil || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return false
	}
	tracker := getAppLiveThreadTracker(a)
	return tracker.Has(sessionKey, threadID)
}

func clearSessionLiveThread(a *App, sessionKey string) {
	if a == nil || strings.TrimSpace(sessionKey) == "" {
		return
	}
	tracker := getAppLiveThreadTracker(a)
	tracker.Clear(sessionKey)
}

func sessionHasInFlightSubmission(sess *state.Session) bool {
	return sessionctx.HasInFlightSubmission(sess)
}

func clearSessionThreadContext(sess *state.Session) {
	sessionctx.ClearThreadContext(sess)
}

func sessionStoreBackendThread(sess *state.Session, backend string) {
	sessionctx.StoreBackendThread(sess, backend)
}

func setSessionThreadContext(sess *state.Session, workspaceID, threadID, name, preview string) {
	sessionctx.SetThreadContext(sess, workspaceID, threadID, name, preview)
}

func effectiveThreadApprovalPolicy(sess *state.Session, ws *config.Workspace) string {
	return sessionctx.EffectiveApprovalPolicy(sess, ws)
}

func effectiveThreadSandboxMode(sess *state.Session, ws *config.Workspace) string {
	return sessionctx.EffectiveSandboxMode(sess, ws)
}

func effectiveThreadServiceTier(sess *state.Session) string {
	return sessionctx.EffectiveServiceTier(sess)
}

func effectiveThreadMultiAgentMode(sess *state.Session, ws *config.Workspace) string {
	return sessionctx.EffectiveMultiAgentMode(sess, ws)
}

func normalizeClaudePermissionModeValue(value string) string {
	switch strings.TrimSpace(value) {
	case "", "default":
		return string(claudePermissionModeDefault)
	case string(claudePermissionModeAcceptEdits):
		return string(claudePermissionModeAcceptEdits)
	case string(claudePermissionModeBypass):
		return string(claudePermissionModeBypass)
	case string(claudePermissionModePlan):
		return string(claudePermissionModePlan)
	default:
		return strings.TrimSpace(value)
	}
}

func effectiveClaudePermissionMode(sess *state.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(sess.ActiveClaudePermissionMode)
	}
	if ws != nil && strings.TrimSpace(ws.ClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(ws.ClaudePermissionMode)
	}
	return normalizeClaudePermissionModeValue(cfg.PermissionMode)
}

func switchSessionWorkspace(sess *state.Session, workspaceID string) {
	sessionctx.SwitchSessionWorkspace(sess, workspaceID)
}

func sessionCanResumeThreadForSubmission(sess *state.Session, sub *state.Submission) bool {
	return sessionctx.CanResumeThreadForSubmission(sess, sub)
}
