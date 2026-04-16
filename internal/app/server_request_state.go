package app

import (
	"strings"

	"feidex/internal/state"
)

func isServerResolvedPendingKind(kind string) bool {
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

func isPendingRequestOpen(req *state.PendingRequest) bool {
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

func (a *App) markPendingRequestReplied(requestID string) *state.PendingRequest {
	appState := a.appState()
	pending := appState.pending(requestID)
	if pending == nil {
		return nil
	}
	nextStatus := "resolved"
	if isServerResolvedPendingKind(pending.Kind) {
		nextStatus = "replied"
	}
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
		req.Status = nextStatus
	})
	return appState.pending(requestID)
}

func (a *App) resolveServerPendingRequest(requestID string) *state.PendingRequest {
	appState := a.appState()
	pending := appState.pending(requestID)
	if pending == nil {
		return nil
	}
	if strings.TrimSpace(pending.Status) == "resolved" {
		return nil
	}
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
		req.Status = "resolved"
	})
	return appState.pending(requestID)
}

func (a *App) hasOpenPendingRequestForTurn(threadID, turnID, excludeID string) bool {
	appState := a.appState()
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	excludeID = strings.TrimSpace(excludeID)
	for _, req := range appState.pendingRequests() {
		if req == nil || !isServerResolvedPendingKind(req.Kind) || !isPendingRequestOpen(req) {
			continue
		}
		if excludeID != "" && strings.TrimSpace(req.ID) == excludeID {
			continue
		}
		if turnID != "" && strings.TrimSpace(req.TurnID) == turnID {
			return true
		}
		if threadID != "" && strings.TrimSpace(req.ThreadID) == threadID {
			return true
		}
	}
	return false
}
