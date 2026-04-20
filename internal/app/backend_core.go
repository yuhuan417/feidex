package app

import (
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const (
	backendCodex  = config.WorkspaceBackendCodex
	backendClaude = config.WorkspaceBackendClaude
)

type sessionInflightMode string

const (
	sessionInflightSingle     sessionInflightMode = "single"
	sessionInflightSerialized sessionInflightMode = "serialized"
	sessionInflightParallel   sessionInflightMode = "parallel"
)

type claudeApprovalResolution struct {
	Behavior  string
	Scope     string
	Message   string
	Interrupt bool
}

func configHasBackend(cfg *config.Config, backend string) bool {
	if cfg == nil {
		return false
	}
	for i := range cfg.Workspaces {
		if workspaceBackend(&cfg.Workspaces[i]) == strings.TrimSpace(backend) {
			return true
		}
	}
	return false
}

func workspaceBackend(ws *config.Workspace) string {
	if ws == nil {
		return backendCodex
	}
	switch strings.TrimSpace(ws.Backend) {
	case backendClaude:
		return backendClaude
	default:
		return backendCodex
	}
}

func workspaceSessionInflightMode(ws *config.Workspace) sessionInflightMode {
	switch workspaceBackend(ws) {
	case backendClaude:
		return sessionInflightSerialized
	default:
		return sessionInflightSingle
	}
}

func sessionInflightAllowsAdditional(mode sessionInflightMode) bool {
	return mode == sessionInflightSerialized || mode == sessionInflightParallel
}

func (a *App) workspaceBackendByID(workspaceID string) string {
	if a == nil || a.cfg == nil {
		return backendCodex
	}
	if ws := config.FindWorkspace(a.cfg, strings.TrimSpace(workspaceID)); ws != nil {
		return workspaceBackend(ws)
	}
	return backendCodex
}

func (a *App) workspaceSessionInflightModeByID(workspaceID string) sessionInflightMode {
	if a == nil || a.cfg == nil {
		return sessionInflightSingle
	}
	if ws := config.FindWorkspace(a.cfg, strings.TrimSpace(workspaceID)); ws != nil {
		return workspaceSessionInflightMode(ws)
	}
	return sessionInflightSingle
}

func (a *App) currentWorkspaceBackendForSessionKey(sessionKey string) string {
	if a == nil {
		return backendCodex
	}
	sess := a.appState().session(strings.TrimSpace(sessionKey))
	if sess == nil {
		return backendCodex
	}
	return a.workspaceBackendByID(sess.WorkspaceID)
}

func (a *App) currentWorkspaceBackendForMessage(msg *feishu.InboundMessage) string {
	if a == nil || msg == nil {
		return backendCodex
	}
	sessionKey := a.makeSessionKey(msg)
	sess := a.appState().session(sessionKey)
	if sess == nil {
		return a.workspaceBackendByID(a.defaultWorkspaceID())
	}
	return a.workspaceBackendByID(sess.WorkspaceID)
}

func submissionBackend(a *App, sub *state.Submission) string {
	if a == nil || sub == nil {
		return backendCodex
	}
	return a.workspaceBackendByID(sub.WorkspaceID)
}

func pendingBackend(a *App, pending *state.PendingRequest) string {
	if pending == nil {
		return backendCodex
	}
	if strings.TrimSpace(pending.Backend) != "" {
		if strings.TrimSpace(pending.Backend) == backendClaude {
			return backendClaude
		}
		return backendCodex
	}
	if a == nil {
		return backendCodex
	}
	if _, sub := a.findSubmissionByTurn(pending.ThreadID, pending.TurnID); sub != nil {
		return submissionBackend(a, sub)
	}
	sess := a.appState().session(pending.SessionKey)
	if sess != nil {
		return a.workspaceBackendByID(sess.WorkspaceID)
	}
	return backendCodex
}

func backendUnsupportedError(label string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "当前功能"
	}
	return errString(label + " 仅支持 Codex backend")
}
