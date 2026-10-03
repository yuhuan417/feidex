package backend

import domainbackend "feidex/internal/domain/backend"

// BackendDisplayName returns a human-readable name for the given backend.
func BackendDisplayName(backend string) string {
	backend = domainbackend.NormalizeBackend(backend)
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
	return domainbackend.NormalizeBackend(value)
}
