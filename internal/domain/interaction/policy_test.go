package interaction

import "testing"

func TestAuthoritativeRequestBoundaries(t *testing.T) {
	for _, kind := range []string{"command", "file", "permissions", "tool_request_user_input", "tool_request_user_input_form", "mcp_elicitation_url", "mcp_elicitation_form"} {
		if !IsServerResolvedPendingKind(kind) || !IsPendingRequestOpen("replied") {
			t.Errorf("%s must remain open after reply until server resolution", kind)
		}
	}
	for _, kind := range []string{"async_user_input", "claude_exit_plan_mode", "upgrade", "unknown"} {
		if IsServerResolvedPendingKind(kind) {
			t.Errorf("%s must not wait for a JSON-RPC resolved notification", kind)
		}
	}
	for _, status := range []string{"", "resolved", "expired", "processing", "cancelling", "upgrading", "unknown"} {
		if IsPendingRequestOpen(status) {
			t.Errorf("%s must not be an open request", status)
		}
	}
}

func TestTurnCleanupPolicy(t *testing.T) {
	cases := []struct {
		backend, kind, status string
		keep                  bool
	}{
		{"codex", "async_user_input", "pending", true},
		{"codex", "async_user_input", "replied", true},
		{"codex", "async_user_input", "resolved", false},
		{"codex", "command", "pending", false},
		{"codex", "mcp_elicitation_form", "replied", false},
		{"claude", "command", "pending", true},
		{"claude", "permissions", "replied", true},
		{"claude", "tool_request_user_input_form", "pending", true},
		{"claude", "claude_exit_plan_mode", "pending", true},
		{"claude", "command", "expired", false},
		{"claude", "async_user_input", "pending", false},
		{" CLAUDE ", " file ", " pending ", true},
	}
	for _, tc := range cases {
		if got := OutlivesTurn(tc.backend, tc.kind, tc.status); got != tc.keep {
			t.Errorf("OutlivesTurn(%q, %q, %q) = %v, want %v", tc.backend, tc.kind, tc.status, got, tc.keep)
		}
	}
}
