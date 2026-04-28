package app

import (
	"strings"
	"testing"

	"feidex/internal/state"
)

func TestServerRequestAdapterForPendingDoesNotFallbackFromStoredUnknownBackend(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendCodex

	pending := &state.PendingRequest{ID: "req-1", Backend: "mystery"}
	adapter := serverRequestAdapterForPending(a, pending)

	if got := adapter.kind(); got != "" {
		t.Fatalf("adapter.kind() = %q, want empty normalized backend", got)
	}
	err := adapter.replyApproval(pending, "", nil)
	if err == nil || !strings.Contains(err.Error(), "backend not configured") {
		t.Fatalf("replyApproval() error = %v, want backend not configured", err)
	}
	if strings.Contains(err.Error(), "codex client not initialized") {
		t.Fatalf("replyApproval() error = %v, want no codex fallback", err)
	}
}

func TestServerRequestAdapterForPendingRejectsUnsetBackend(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = ""

	err := serverRequestAdapterForPending(a, &state.PendingRequest{ID: "req-1"}).replyApproval(&state.PendingRequest{ID: "req-1"}, "", nil)
	if err == nil || !strings.Contains(err.Error(), "backend not configured") {
		t.Fatalf("replyApproval() error = %v, want backend not configured", err)
	}
}
