package submission

import (
	"encoding/json"
	"sort"

	"feidex/internal/app/pendingforms"
	"feidex/internal/state"
)

// Type aliases to pendingforms types.
type (
	ToolUserInputOption      = pendingforms.ToolUserInputOption
	ToolUserInputQuestion    = pendingforms.ToolUserInputQuestion
	ToolUserInputPayload     = pendingforms.ToolUserInputPayload
	FormDrafts               = pendingforms.FormDrafts
	ElicitationFormPayload   = pendingforms.ElicitationFormPayload
	ElicitationURLPayload    = pendingforms.ElicitationURLPayload
)

// ParseStructuredLines parses "key: value" lines into a map.
func ParseStructuredLines(text string) map[string]string {
	return pendingforms.ParseStructuredLines(text)
}

// PendingTextRequest returns the most recent pending text request for the
// session/user pair, or nil if none. The caller provides the slice of all
// pending requests.
func PendingTextRequest(allPending []*state.PendingRequest, sessionKey, userID string, pendingKinds map[string]bool) *state.PendingRequest {
	sort.Slice(allPending, func(i, j int) bool { return allPending[i].CreatedAt > allPending[j].CreatedAt })
	for _, req := range allPending {
		if req == nil || req.Status != "pending" || req.SessionKey != sessionKey {
			continue
		}
		if req.OwnerUserID != "" && req.OwnerUserID != userID {
			continue
		}
		if pendingKinds[req.Kind] {
			return req
		}
	}
	return nil
}

// ShouldRedactInboundText returns true if the inbound text should be redacted
// (e.g. when a pending form with secrets is active).
func ShouldRedactInboundText(pending *state.PendingRequest) bool {
	if pending == nil {
		return false
	}
	switch pending.Kind {
	case "mcp_elicitation_form":
		return true
	case "tool_request_user_input_form":
		var payload ToolUserInputPayload
		if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
			return true
		}
		for _, q := range payload.Questions {
			if q.IsSecret {
				return true
			}
		}
	}
	return false
}

// CancelledPendingTitle returns the title for a cancelled pending request.
// planModeKind and reviewKind are the kind strings for plan mode and review
// pending requests (passed to avoid importing app-level constants).
func CancelledPendingTitle(pending *state.PendingRequest, planModeKind, reviewKind string) string {
	if pending == nil {
		return "已取消"
	}
	switch pending.Kind {
	case planModeKind:
		return "计划确认已取消"
	case "tool_request_user_input_form":
		return "输入请求已取消"
	case "mcp_elicitation_form":
		return "表单请求已取消"
	case reviewKind:
		return "Review 已取消"
	default:
		return "已取消"
	}
}

// RenderToolUserInputBody wraps pendingforms.RenderToolUserInputBody.
func RenderToolUserInputBody(payload ToolUserInputPayload) string {
	return pendingforms.RenderToolUserInputBody(payload)
}

// RenderElicitationFormBody wraps pendingforms.RenderElicitationFormBody.
func RenderElicitationFormBody(payload ElicitationFormPayload) string {
	return pendingforms.RenderElicitationFormBody(payload)
}
