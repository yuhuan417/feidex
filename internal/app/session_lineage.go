package app

import (
	"strings"

	"feidex/internal/config"
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
	if sessionHasActiveOperations(sess) {
		return true
	}
	return strings.TrimSpace(sess.ActiveTurnID) != "" || strings.TrimSpace(sess.ActiveSubmissionID) != ""
}

func clearSessionThreadContext(sess *state.Session) {
	if sess == nil {
		return
	}
	sess.ActiveThreadID = ""
	sess.ActiveThreadWorkspaceID = ""
	sess.ActiveThreadApprovalPolicy = ""
	sess.ActiveThreadSandboxMode = ""
	sess.ActiveClaudePermissionMode = ""
	sess.ActiveThreadServiceTier = ""
	sess.ActiveThreadName = ""
	sess.ActiveThreadPreview = ""
}

func sessionBackendThreadSnapshot(sess *state.Session) state.SessionBackendThread {
	if sess == nil {
		return state.SessionBackendThread{}
	}
	return state.SessionBackendThread{
		ThreadID:             strings.TrimSpace(sess.ActiveThreadID),
		WorkspaceID:          strings.TrimSpace(sess.ActiveThreadWorkspaceID),
		ApprovalPolicy:       strings.TrimSpace(sess.ActiveThreadApprovalPolicy),
		SandboxMode:          strings.TrimSpace(sess.ActiveThreadSandboxMode),
		ClaudePermissionMode: strings.TrimSpace(sess.ActiveClaudePermissionMode),
		ServiceTier:          strings.TrimSpace(sess.ActiveThreadServiceTier),
		Name:                 strings.TrimSpace(sess.ActiveThreadName),
		Preview:              strings.TrimSpace(sess.ActiveThreadPreview),
	}
}

func sessionStoreBackendThread(sess *state.Session, backend string) {
	if sess == nil {
		return
	}
	backend = normalizeRuntimeBackend(backend)
	if backend == "" {
		return
	}
	if sess.BackendThreads == nil {
		sess.BackendThreads = map[string]state.SessionBackendThread{}
	}
	snapshot := sessionBackendThreadSnapshot(sess)
	if snapshot == (state.SessionBackendThread{}) {
		delete(sess.BackendThreads, backend)
		if len(sess.BackendThreads) == 0 {
			sess.BackendThreads = nil
		}
		return
	}
	sess.BackendThreads[backend] = snapshot
}

func sessionClearBackendThread(sess *state.Session, backend string) {
	if sess == nil {
		return
	}
	backend = normalizeRuntimeBackend(backend)
	if backend == "" || len(sess.BackendThreads) == 0 {
		return
	}
	delete(sess.BackendThreads, backend)
	if len(sess.BackendThreads) == 0 {
		sess.BackendThreads = nil
	}
}

func sessionRestoreBackendThread(sess *state.Session, backend string) bool {
	if sess == nil {
		return false
	}
	backend = normalizeRuntimeBackend(backend)
	if backend == "" || len(sess.BackendThreads) == 0 {
		clearSessionThreadContext(sess)
		return false
	}
	snapshot, ok := sess.BackendThreads[backend]
	if !ok {
		clearSessionThreadContext(sess)
		return false
	}
	if strings.TrimSpace(snapshot.WorkspaceID) != "" {
		sess.WorkspaceID = strings.TrimSpace(snapshot.WorkspaceID)
	}
	setSessionThreadContext(sess, snapshot.WorkspaceID, snapshot.ThreadID, snapshot.Name, snapshot.Preview)
	sess.ActiveThreadApprovalPolicy = strings.TrimSpace(snapshot.ApprovalPolicy)
	sess.ActiveThreadSandboxMode = strings.TrimSpace(snapshot.SandboxMode)
	sess.ActiveClaudePermissionMode = strings.TrimSpace(snapshot.ClaudePermissionMode)
	sess.ActiveThreadServiceTier = normalizeServiceTier(snapshot.ServiceTier)
	return true
}

func clearSessionBackendThreads(sess *state.Session) {
	if sess == nil {
		return
	}
	sess.BackendThreads = nil
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

func setSessionThreadDefaults(sess *state.Session, approvalPolicy, sandboxMode string) {
	if sess == nil {
		return
	}
	sess.ActiveThreadApprovalPolicy = strings.TrimSpace(approvalPolicy)
	sess.ActiveThreadSandboxMode = strings.TrimSpace(sandboxMode)
}

func effectiveThreadApprovalPolicy(sess *state.Session, ws *config.Workspace) string {
	if sess != nil && strings.TrimSpace(sess.ActiveThreadApprovalPolicy) != "" {
		return strings.TrimSpace(sess.ActiveThreadApprovalPolicy)
	}
	if ws != nil {
		return strings.TrimSpace(ws.ApprovalPolicy)
	}
	return ""
}

func effectiveThreadSandboxMode(sess *state.Session, ws *config.Workspace) string {
	if sess != nil && strings.TrimSpace(sess.ActiveThreadSandboxMode) != "" {
		return strings.TrimSpace(sess.ActiveThreadSandboxMode)
	}
	if ws != nil {
		return strings.TrimSpace(ws.SandboxMode)
	}
	return ""
}

func effectiveThreadServiceTier(sess *state.Session) string {
	if sess != nil {
		return normalizeServiceTier(sess.ActiveThreadServiceTier)
	}
	return ""
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
	if sess == nil {
		return
	}
	previousWorkspaceID := strings.TrimSpace(sess.WorkspaceID)
	sess.WorkspaceID = strings.TrimSpace(workspaceID)
	if !sessionHasInFlightSubmission(sess) {
		clearSessionBackendThreads(sess)
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
