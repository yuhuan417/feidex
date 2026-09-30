package lifecycle

import (
	"strings"

	appapproval "feidex/internal/app/approval"
	"feidex/internal/app/pendingforms"
	appruntime "feidex/internal/app/runtime"
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

// IsClaudeInteractiveKind reports whether the kind belongs to a Claude
// interactive request (permission approval, question, plan confirmation).
func IsClaudeInteractiveKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case appapproval.KindCommand.String(),
		appapproval.KindFile.String(),
		appapproval.KindPermissions.String(),
		"tool_request_user_input",
		"tool_request_user_input_form",
		"claude_exit_plan_mode":
		return true
	default:
		return false
	}
}

// OutlivesTurn reports whether an open pending request must survive cleanup of
// the turn that produced it.
//
// Two families do:
//   - Codex async questions, which are answered as ordinary conversation input.
//   - Claude interactive requests, whose continuation boundary is the control
//     response written back to the CLI, not the turn. Background agents
//     routinely outlive the turn that spawned them, and a request can also
//     arrive while the user is still deciding after the turn completed.
//
// Deleting those here would leave a card that can never be answered and, on
// the Claude side, a request the CLI waits on forever.
func OutlivesTurn(req *state.PendingRequest) bool {
	if !IsPendingRequestOpen(req) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(req.Backend), appruntime.BackendClaude) {
		return IsClaudeInteractiveKind(req.Kind)
	}
	return strings.TrimSpace(req.Kind) == pendingforms.AsyncUserInputPendingKind
}
