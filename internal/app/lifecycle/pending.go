package lifecycle

import (
	"strings"

	appapproval "feidex/internal/app/approval"
	"feidex/internal/state"
)

func IsServerResolvedPendingKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case appapproval.KindCommand.String(),
		appapproval.KindFile.String(),
		appapproval.KindPermissions.String(),
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
	switch state.NormalizePendingRequestStatus(req.Status) {
	case state.PendingRequestStatusPending, state.PendingRequestStatusReplied:
		return true
	default:
		return false
	}
}
