package feishuapp

import (
	domainbackend "feidex/internal/domain/backend"
	"strings"

	"feidex/internal/state"
)

type sessionInflightMode = domainbackend.SessionInflightMode

const (
	sessionInflightSingle     sessionInflightMode = domainbackend.SessionInflightSingle
	sessionInflightSerialized sessionInflightMode = domainbackend.SessionInflightSerialized
	sessionInflightParallel   sessionInflightMode = domainbackend.SessionInflightParallel
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
	a.invalidateBackendConfigurationService()
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
