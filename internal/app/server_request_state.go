package app

import (
	storagejson "feidex/internal/adapter/storage/json"
	applicationinteraction "feidex/internal/application/interaction"
	"feidex/internal/domain/interaction"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/state"
)

func isServerResolvedPendingKind(kind string) bool {
	return interaction.IsServerResolvedPendingKind(kind)
}

func isPendingRequestOpen(req *state.PendingRequest) bool {
	return storagejson.IsPendingRequestOpen(req)
}

func (s runtimeStateService) interactionService() applicationinteraction.Service {
	store := s.app.State()
	return applicationinteraction.Service{Deps: applicationinteraction.Dependencies{Repository: storagejson.InteractionRepository{
		Store: store.StateStore(), FrontendID: store.FrontendID(), LegacyFallback: store.LegacyFallbackEnabled(),
	}}}
}

func (s runtimeStateService) resolveServerPendingRequest(requestID string) *state.PendingRequest {
	request, _ := s.interactionService().Resolve(requestID)
	if request == nil {
		return nil
	}
	return s.app.State().Pending(request.ID)
}

// resumeSubmissionAfterRequest is the narrow interaction -> submission
// handoff. Resolution is authoritative first; only when the interaction
// application service reports no other open request may the submission return
// to running. It does not call back through ServerRequestService.
func (s runtimeStateService) resumeSubmissionAfterRequest(pending *state.PendingRequest) {
	if pending == nil || s.hasOpenPendingRequestForTurn(pending.ThreadID, pending.TurnID, pending.ID) {
		return
	}
	_, sub := findSubmissionByTurn(s.app, pending.ThreadID, pending.TurnID)
	if sub == nil {
		return
	}
	_ = s.app.State().SetSubmissionStatus(sub.ID, domainsubmission.SubmissionStatusRunning.String())
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
		resolved := s.resolveServerPendingRequest(pending.ID)
		s.resumeSubmissionAfterRequest(pending)
		return resolved
	}
	request, _ := s.interactionService().ReplyAccepted(pending.ID)
	if request == nil {
		return nil
	}
	return s.app.State().Pending(request.ID)
}

func (s runtimeStateService) hasOpenPendingRequestForTurn(threadID, turnID, excludeID string) bool {
	return s.interactionService().HasOpenRequest(threadID, turnID, excludeID)
}
