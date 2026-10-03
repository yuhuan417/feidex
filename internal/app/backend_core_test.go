package app

import (
	domainbackend "feidex/internal/domain/backend"
	"testing"

	"feidex/internal/state"
)

func TestPendingBackendPrefersStoredPendingBackend(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude

	if got := pendingBackend(a, &state.PendingRequest{Backend: domainbackend.BackendCodex}); got != domainbackend.BackendCodex {
		t.Fatalf("pendingBackend(stored codex) = %q, want %q", got, domainbackend.BackendCodex)
	}
	if got := pendingBackend(a, &state.PendingRequest{}); got != domainbackend.BackendClaude {
		t.Fatalf("pendingBackend(fallback configured) = %q, want %q", got, domainbackend.BackendClaude)
	}
}
