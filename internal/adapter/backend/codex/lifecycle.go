package codex

import (
	"encoding/json"
	"strings"

	"feidex/internal/application"
	"feidex/internal/codexrpc"
)

// DecodeLifecycle maps wire notifications into semantic application events.
// The boolean distinguishes notifications handled here from other protocol
// families that are still served by the legacy router during migration.
func DecodeLifecycle(method string, params json.RawMessage) (application.BackendEvent, bool, error) {
	event := application.BackendEvent{}
	switch method {
	case "turn/started":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Turn     struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return event, true, err
		}
		turnID := strings.TrimSpace(p.Turn.ID)
		if turnID == "" {
			turnID = strings.TrimSpace(p.TurnID)
		}
		return application.BackendEvent{Kind: application.EventTurnStarted, ThreadID: p.ThreadID, TurnID: turnID}, true, nil
	case "turn/completed":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string                        `json:"id"`
				Status string                        `json:"status"`
				Error  *codexrpc.ThreadReadTurnError `json:"error"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return event, true, err
		}
		return application.BackendEvent{Kind: application.EventTurnCompleted, ThreadID: p.ThreadID, TurnID: p.Turn.ID, Status: p.Turn.Status, Message: p.Turn.Error.DisplayText()}, true, nil
	case "error":
		var p struct {
			ThreadID string                       `json:"threadId"`
			TurnID   string                       `json:"turnId"`
			Error    codexrpc.ThreadReadTurnError `json:"error"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return event, true, err
		}
		return application.BackendEvent{Kind: application.EventTurnError, ThreadID: p.ThreadID, TurnID: p.TurnID, Message: p.Error.DisplayText()}, true, nil
	case "serverRequest/resolved":
		var p struct {
			ThreadID  string          `json:"threadId"`
			RequestID json.RawMessage `json:"requestId"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return event, true, err
		}
		return application.BackendEvent{Kind: application.EventRequestResolved, ThreadID: p.ThreadID, RequestID: RequestIDKey(p.RequestID)}, true, nil
	default:
		return event, false, nil
	}
}
