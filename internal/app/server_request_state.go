package app

import (
	"strings"

	applifecycle "feidex/internal/app/lifecycle"
	"feidex/internal/state"
)

func isServerResolvedPendingKind(kind string) bool {
	return applifecycle.IsServerResolvedPendingKind(kind)
}

func isPendingRequestOpen(req *state.PendingRequest) bool {
	return applifecycle.IsPendingRequestOpen(req)
}

func (s runtimeStateService) markPendingRequestReplied(requestID string) *state.PendingRequest {
	appState := s.app.State()
	pending := appState.Pending(requestID)
	if pending == nil {
		return nil
	}
	nextStatus := state.PendingRequestStatusResolved.String()
	if isServerResolvedPendingKind(pending.Kind) {
		nextStatus = state.PendingRequestStatusReplied.String()
	}
	_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = nextStatus
	})
	return appState.Pending(requestID)
}

func (s runtimeStateService) markPendingRequestResolved(requestID string) *state.PendingRequest {
	appState := s.app.State()
	pending := appState.Pending(requestID)
	if pending == nil {
		return nil
	}
	if state.NormalizePendingRequestStatus(pending.Status) == state.PendingRequestStatusResolved {
		return nil
	}
	_ = appState.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusResolved.String()
	})
	return appState.Pending(requestID)
}

func (s runtimeStateService) resolveServerPendingRequest(requestID string) *state.PendingRequest {
	return s.markPendingRequestResolved(requestID)
}

func (s runtimeStateService) backendResolvesPendingLocally(pending *state.PendingRequest) bool {
	if pending == nil {
		return false
	}
	if runtime := backendRuntimeForKind(pendingBackend(s.app, pending)); runtime != nil {
		return runtime.resolvesPendingLocally(pending.Kind)
	}
	return !isServerResolvedPendingKind(pending.Kind)
}

func (s runtimeStateService) finalizePendingReply(pending *state.PendingRequest) *state.PendingRequest {
	if pending == nil {
		return nil
	}
	if s.backendResolvesPendingLocally(pending) {
		resolved := s.markPendingRequestResolved(pending.ID)
		resumeSubmissionAfterRequest(s.app, pending)
		return resolved
	}
	return s.markPendingRequestReplied(pending.ID)
}

func (s runtimeStateService) hasOpenPendingRequestForTurn(threadID, turnID, excludeID string) bool {
	appState := s.app.State()
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	excludeID = strings.TrimSpace(excludeID)
	for _, req := range appState.PendingRequests() {
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
