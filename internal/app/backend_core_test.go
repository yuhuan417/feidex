package app

import (
	"testing"

	"feidex/internal/state"
)

func TestPendingBackendPrefersStoredPendingBackend(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude

	if got := pendingBackend(a, &state.PendingRequest{Backend: backendCodex}); got != backendCodex {
		t.Fatalf("pendingBackend(stored codex) = %q, want %q", got, backendCodex)
	}
	if got := pendingBackend(a, &state.PendingRequest{}); got != backendClaude {
		t.Fatalf("pendingBackend(fallback configured) = %q, want %q", got, backendClaude)
	}
}
