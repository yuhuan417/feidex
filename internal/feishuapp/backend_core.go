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

func configuredSessionInflightMode(backend func() string) sessionInflightMode {
	if backend == nil {
		return sessionInflightSingle
	}
	return sessionInflightModeForBackend(backend())
}

func pendingBackend(view frontendConfigView, pending *state.PendingRequest) string {
	if pending != nil && strings.TrimSpace(pending.Backend) != "" {
		return normalizeRuntimeBackend(pending.Backend)
	}
	if backend := view.configuredBackend(); backend != "" {
		return backend
	}
	return ""
}
