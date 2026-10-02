package app

import (
	domainbackend "feidex/internal/domain/backend"
	"strings"

	appruntime "feidex/internal/runtime"
	"feidex/internal/state"
)

const (
	backendCodex  = domainbackend.BackendCodex
	backendClaude = domainbackend.BackendClaude
)

type sessionInflightMode = domainbackend.SessionInflightMode

const (
	sessionInflightSingle     sessionInflightMode = domainbackend.SessionInflightSingle
	sessionInflightSerialized sessionInflightMode = domainbackend.SessionInflightSerialized
	sessionInflightParallel   sessionInflightMode = domainbackend.SessionInflightParallel
)

const (
	claudePermissionModeDefault     = appruntime.ClaudePermissionModeDefault
	claudePermissionModeAcceptEdits = appruntime.ClaudePermissionModeAcceptEdits
	claudePermissionModePlan        = appruntime.ClaudePermissionModePlan
	claudePermissionModeBypass      = appruntime.ClaudePermissionModeBypass
)

func sessionInflightModeForBackend(backend string) sessionInflightMode {
	return domainbackend.SessionInflightModeForBackend(backend)
}

func sessionInflightAllowsAdditional(mode sessionInflightMode) bool {
	return domainbackend.SessionInflightAllowsAdditional(mode)
}

func setRuntimeBackend(a *App, backend string) {
	if a == nil {
		return
	}
	a.configMutex().Lock()
	defer a.configMutex().Unlock()
	a.backend = normalizeRuntimeBackend(backend)
	a.backendDriver = backendDriverForKind(a.backend)
	a.invalidateThreadMenuService()
	if a.stateView != nil {
		a.stateView.SetBackend(a.backend)
	}
}

func configuredSessionInflightMode(a *App) sessionInflightMode {
	return sessionInflightModeForBackend(configuredBackend(a))
}

func pendingBackend(a *App, pending *state.PendingRequest) string {
	if pending != nil && strings.TrimSpace(pending.Backend) != "" {
		return normalizeRuntimeBackend(pending.Backend)
	}
	if a != nil {
		if backend := configuredBackend(a); backend != "" {
			return backend
		}
	}
	return ""
}
