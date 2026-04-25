package backend

import (
	"feidex/internal/app/appcore"
)

// BackendDisplayName returns a human-readable name for the given backend.
func BackendDisplayName(backend string) string {
	backend = appcore.NormalizeRuntimeBackend(backend)
	switch backend {
	case "codex":
		return "Codex"
	case "claude":
		return "Claude"
	default:
		return "未设置"
	}
}

// NormalizeRuntimeBackend normalizes a backend name to its canonical form.
func NormalizeRuntimeBackend(value string) string {
	return appcore.NormalizeRuntimeBackend(value)
}
