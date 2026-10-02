package serverrequest

import (
	"encoding/json"
	codexadapter "feidex/internal/adapter/backend/codex"
	"testing"

	"feidex/internal/state"
)

func TestRequestIDHelpers(t *testing.T) {
	if got := codexadapter.RequestIDKey(json.RawMessage(` "req-1" `)); got != "req-1" {
		t.Fatalf("codexadapter.RequestIDKey(json string) = %q, want req-1", got)
	}
	if got := codexadapter.RequestIDKey(json.RawMessage(`req-2`)); got != "req-2" {
		t.Fatalf("codexadapter.RequestIDKey(raw) = %q, want req-2", got)
	}
	if got := requestIDRaw(" req-3 "); string(got) != `"req-3"` {
		t.Fatalf("requestIDRaw() = %q, want quoted req-3", string(got))
	}
	if got := requestIDStored(json.RawMessage(` "req-4" `)); got != `"req-4"` {
		t.Fatalf("requestIDStored() = %q, want stored raw string", got)
	}
	if got := pendingRequestIDRaw(&state.PendingRequest{ID: "req-5", RequestIDRaw: ` "req-5" `}); string(got) != `"req-5"` {
		t.Fatalf("pendingRequestIDRaw(stored) = %q", string(got))
	}
	if got := pendingRequestIDRaw(&state.PendingRequest{ID: "req-6"}); string(got) != `"req-6"` {
		t.Fatalf("pendingRequestIDRaw(fallback) = %q", string(got))
	}
}
