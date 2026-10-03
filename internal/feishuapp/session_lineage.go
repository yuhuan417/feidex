package feishuapp

import (
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	identity "feidex/internal/domain/identity"
	domainsubmission "feidex/internal/domain/submission"
	frontendruntime "feidex/internal/runtime"
	"strings"
)

func getAppLiveThreadTracker(a *App) *frontendruntime.LiveThreads {
	if a == nil {
		return nil
	}
	owner := ensureRuntimeOwner(a)
	if owner.LiveThreads == nil {
		owner.LiveThreads = frontendruntime.NewLiveThreads()
	}
	return owner.LiveThreads
}

func resetAppLiveThreadTracker(a *App) {
	if a == nil {
		return
	}
	owner := ensureRuntimeOwner(a)
	owner.LiveThreads = frontendruntime.NewLiveThreads()
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
			_, _, chatID, _, _ = identity.ParseSessionKey(sess.Key)
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
		return string(frontendruntime.ClaudePermissionModeDefault)
	case string(frontendruntime.ClaudePermissionModeAcceptEdits):
		return string(frontendruntime.ClaudePermissionModeAcceptEdits)
	case string(frontendruntime.ClaudePermissionModeBypass):
		return string(frontendruntime.ClaudePermissionModeBypass)
	case string(frontendruntime.ClaudePermissionModePlan):
		return string(frontendruntime.ClaudePermissionModePlan)
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
