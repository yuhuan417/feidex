package serverrequest

import (
	"encoding/json"
	"sort"

	"feidex/internal/app/pendingforms"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// PendingTextRequest finds the most recent open pending text request for a session/user.
func (s *Service) PendingTextRequest(sessionKey, userID string) *state.PendingRequest {
	pending := s.PendingRequests()
	sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt > pending[j].CreatedAt })
	for _, req := range pending {
		if req == nil || state.NormalizePendingRequestStatus(req.Status) != state.PendingRequestStatusPending || !s.sameSessionKey(req.SessionKey, sessionKey) {
			continue
		}
		if req.OwnerUserID != "" && req.OwnerUserID != userID {
			continue
		}
		switch req.Kind {
		case "tool_request_user_input_form", "mcp_elicitation_form":
			return req
		}
	}
	return nil
}

func (s *Service) sameSessionKey(a, b string) bool {
	if s != nil && s.SessionKeysEqual != nil {
		return s.SessionKeysEqual(a, b)
	}
	return a == b
}

// ShouldRedactInboundText reports whether inbound text should be hidden
// because a secret pending input is active.
func (s *Service) ShouldRedactInboundText(sessionKey, userID string) bool {
	req := s.PendingTextRequest(sessionKey, userID)
	if req == nil {
		return false
	}
	switch req.Kind {
	case "mcp_elicitation_form":
		return true
	case "tool_request_user_input_form":
		var payload ToolUserInputPayload
		if err := json.Unmarshal([]byte(req.PayloadJSON), &payload); err != nil {
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

// HandlePendingTextResponse dispatches a text reply to the appropriate handler
// based on the pending request kind.
func (s *Service) HandlePendingTextResponse(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	if msg == nil || pending == nil {
		return nil
	}
	switch pending.Kind {
	case "tool_request_user_input_form":
		return s.CompleteToolUserInputText(msg, pending)
	case "mcp_elicitation_form":
		return s.CompleteElicitationFormText(msg, pending)
	default:
		return nil
	}
}

// ParseStructuredLines parses "key: value" lines into a map.
func ParseStructuredLines(text string) map[string]string {
	return pendingforms.ParseStructuredLines(text)
}
