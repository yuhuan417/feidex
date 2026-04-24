package app

import (
	"strings"

	"feidex/internal/config"
	"feidex/internal/state"
)

const (
	backendCodex  = config.RuntimeBackendCodex
	backendClaude = config.RuntimeBackendClaude
)

type sessionInflightMode string

const (
	sessionInflightSingle     sessionInflightMode = "single"
	sessionInflightSerialized sessionInflightMode = "serialized"
	sessionInflightParallel   sessionInflightMode = "parallel"
)

type claudeApprovalResolution struct {
	Behavior           string
	Scope              string
	Message            string
	Interrupt          bool
	UpdatedPermissions []map[string]any
}

type claudePermissionMode string

const (
	claudePermissionModeDefault     claudePermissionMode = "default"
	claudePermissionModeAcceptEdits claudePermissionMode = "acceptEdits"
	claudePermissionModePlan        claudePermissionMode = "plan"
	claudePermissionModeBypass      claudePermissionMode = "bypassPermissions"
)

func normalizeRuntimeBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case backendClaude:
		return backendClaude
	case backendCodex:
		return backendCodex
	default:
		return ""
	}
}

func sessionInflightModeForBackend(string) sessionInflightMode {
	return sessionInflightSingle
}

func sessionInflightAllowsAdditional(mode sessionInflightMode) bool {
	return mode == sessionInflightSerialized || mode == sessionInflightParallel
}

func (a *App) configuredBackend() string {
	if a == nil {
		return ""
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	if backend := normalizeRuntimeBackend(a.backend); strings.TrimSpace(a.backend) != "" {
		return backend
	}
	if cfg := a.feishuConfigUnlocked(); cfg != nil {
		return normalizeRuntimeBackend(cfg.Backend)
	}
	return ""
}

func (a *App) currentRuntimeBackend() string {
	if a == nil {
		return ""
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return normalizeRuntimeBackend(a.backend)
}

func (a *App) setRuntimeBackend(backend string) {
	if a == nil {
		return
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.backend = normalizeRuntimeBackend(backend)
}

func (a *App) hasConfiguredBackend() bool {
	return strings.TrimSpace(a.configuredBackend()) != ""
}

func (a *App) configuredSessionInflightMode() sessionInflightMode {
	return sessionInflightModeForBackend(a.configuredBackend())
}

func pendingBackend(a *App, pending *state.PendingRequest) string {
	if pending != nil && strings.TrimSpace(pending.Backend) != "" {
		return normalizeRuntimeBackend(pending.Backend)
	}
	if a != nil {
		if backend := a.configuredBackend(); backend != "" {
			return backend
		}
	}
	return ""
}
