package app

import (
	storagejson "feidex/internal/adapter/storage/json"
	applifecycle "feidex/internal/app/lifecycle"
	applicationinteraction "feidex/internal/application/interaction"
	"feidex/internal/domain/interaction"
	"feidex/internal/state"
)

func isServerResolvedPendingKind(kind string) bool {
	return interaction.IsServerResolvedPendingKind(kind)
}

func isPendingRequestOpen(req *state.PendingRequest) bool {
	return applifecycle.IsPendingRequestOpen(req)
}

func (s runtimeStateService) interactionService() applicationinteraction.Service {
	store := s.app.State()
	return applicationinteraction.Service{Repository: storagejson.InteractionRepository{
		Store: store.Store, FrontendID: store.FrontendID, LegacyFallback: store.LegacyFallback,
	}}
}

func (s runtimeStateService) resolveServerPendingRequest(requestID string) *state.PendingRequest {
	request, _ := s.interactionService().Resolve(requestID)
	if request == nil {
		return nil
	}
	return s.app.State().Pending(request.ID)
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
		s.app.ServerRequestService().ResumeSubmissionAfterRequest(pending)
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
