package app

import (
	"strings"

	appruntime "feidex/internal/app/runtime"
	"feidex/internal/state"
)

const (
	backendCodex  = appruntime.BackendCodex
	backendClaude = appruntime.BackendClaude
)

type sessionInflightMode = appruntime.SessionInflightMode

const (
	sessionInflightSingle     sessionInflightMode = appruntime.SessionInflightSingle
	sessionInflightSerialized sessionInflightMode = appruntime.SessionInflightSerialized
	sessionInflightParallel   sessionInflightMode = appruntime.SessionInflightParallel
)

type claudeApprovalResolution = appruntime.ClaudeApprovalResolution

type claudePermissionMode = appruntime.ClaudePermissionMode

const (
	claudePermissionModeDefault     = appruntime.ClaudePermissionModeDefault
	claudePermissionModeAcceptEdits = appruntime.ClaudePermissionModeAcceptEdits
	claudePermissionModePlan        = appruntime.ClaudePermissionModePlan
	claudePermissionModeBypass      = appruntime.ClaudePermissionModeBypass
)

func normalizeRuntimeBackend(value string) string {
	return appruntime.NormalizeBackend(value)
}

func sessionInflightModeForBackend(backend string) sessionInflightMode {
	return appruntime.SessionInflightModeForBackend(backend)
}

func sessionInflightAllowsAdditional(mode sessionInflightMode) bool {
	return appruntime.SessionInflightAllowsAdditional(mode)
}

func configuredBackend(a *App) string {
	if a == nil {
		return ""
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	if backend := normalizeRuntimeBackend(a.backend); strings.TrimSpace(a.backend) != "" {
		return backend
	}
	if cfg := feishuConfigUnlocked(a); cfg != nil {
		return normalizeRuntimeBackend(cfg.Backend)
	}
	return ""
}

func currentRuntimeBackend(a *App) string {
	if a == nil {
		return ""
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return normalizeRuntimeBackend(a.backend)
}

func setRuntimeBackend(a *App, backend string) {
	if a == nil {
		return
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.backend = normalizeRuntimeBackend(backend)
}

func hasConfiguredBackend(a *App) bool {
	return strings.TrimSpace(configuredBackend(a)) != ""
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
