package app

import (
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"

	"feidex/internal/config"
	frontendruntime "feidex/internal/runtime"
	"strings"
)

func getAppLiveThreadTracker(a *App) *frontendruntime.LiveThreads {
	if a == nil {
		return nil
	}
	if a.liveThreads != nil {
		return a.liveThreads
	}
	if a.composition != nil {
		if a.composition.liveThreads == nil {
			a.composition.liveThreads = frontendruntime.NewLiveThreads()
		}
		return a.composition.liveThreads
	}
	if a.liveThreads == nil {
		a.liveThreads = frontendruntime.NewLiveThreads()
	}
	return a.liveThreads
}

func resetAppLiveThreadTracker(a *App) {
	if a == nil {
		return
	}
	if a.composition != nil {
		a.composition.liveThreads = frontendruntime.NewLiveThreads()
		return
	}
	a.liveThreads = frontendruntime.NewLiveThreads()
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

func effectiveThreadApprovalPolicy(sess *conversation.Session, ws *config.Workspace) string {
	workspaceValue := ""
	if ws != nil {
		workspaceValue = ws.ApprovalPolicy
	}
	return conversation.EffectiveApprovalPolicy(sess, workspaceValue)
}

func effectiveThreadSandboxMode(sess *conversation.Session, ws *config.Workspace) string {
	workspaceValue := ""
	if ws != nil {
		workspaceValue = ws.SandboxMode
	}
	return conversation.EffectiveSandboxMode(sess, workspaceValue)
}

func effectiveThreadMultiAgentMode(sess *conversation.Session, ws *config.Workspace) string {
	workspaceValue := ""
	if ws != nil {
		workspaceValue = ws.MultiAgentMode
	}
	return conversation.EffectiveMultiAgentMode(sess, workspaceValue)
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

func effectiveClaudePermissionMode(sess *conversation.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(sess.ActiveClaudePermissionMode)
	}
	if ws != nil && strings.TrimSpace(ws.ClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(ws.ClaudePermissionMode)
	}
	return normalizeClaudePermissionModeValue(cfg.PermissionMode)
}

func sessionCanResumeThreadForSubmission(sess *conversation.Session, sub *domainsubmission.Submission) bool {
	return (sub != nil && conversation.CanResumeThreadForWorkspace(sess, sub.WorkspaceID))
}
