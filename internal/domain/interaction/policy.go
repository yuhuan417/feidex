// Package interaction owns human interaction state and lifecycle policy.
package interaction

import "strings"

// IsServerResolvedPendingKind identifies requests whose authoritative terminal
// boundary is the backend's serverRequest/resolved notification. Sending a
// reply only advances them to replied; it never closes them.
func IsServerResolvedPendingKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "command", "file", "permissions", "tool_request_user_input",
		"tool_request_user_input_form", "mcp_elicitation_url", "mcp_elicitation_form":
		return true
	default:
		return false
	}
}

func IsPendingRequestOpen(status string) bool {
	switch strings.TrimSpace(status) {
	case "pending", "replied":
		return true
	default:
		return false
	}
}

func IsClaudeInteractiveKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "command", "file", "permissions", "tool_request_user_input",
		"tool_request_user_input_form", "claude_exit_plan_mode":
		return true
	default:
		return false
	}
}

// OutlivesTurn preserves open asynchronous questions and Claude control
// requests after their originating turn finishes. Claude background work can
// still be waiting on a control response; async questions use ordinary input
// and have no JSON-RPC resolved boundary.
func OutlivesTurn(backend, kind, status string) bool {
	if !IsPendingRequestOpen(status) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(backend), "claude") {
		return IsClaudeInteractiveKind(kind)
	}
	return strings.TrimSpace(kind) == "async_user_input"
}
