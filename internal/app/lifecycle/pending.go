package lifecycle

import (
	"strings"

	"feidex/internal/state"
)

func IsServerResolvedPendingKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "command",
		"file",
		"permissions",
		"tool_request_user_input",
		"tool_request_user_input_form",
		"mcp_elicitation_url",
		"mcp_elicitation_form":
		return true
	default:
		return false
	}
}

func IsPendingRequestOpen(req *state.PendingRequest) bool {
	if req == nil {
		return false
	}
	switch strings.TrimSpace(req.Status) {
	case "pending", "replied":
		return true
	default:
		return false
	}
}
