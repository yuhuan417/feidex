package app

import (
	"strings"

	"feidex/internal/state"
)

func (s *appStateFacade) pending(id string) *state.PendingRequest {
	if s == nil || s.store == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if req := s.store.PendingByScopedID(s.frontendID, id); req != nil {
		return req
	}
	if s.legacyFallback && s.frontendID != "" {
		return s.store.PendingByID(id)
	}
	return nil
}

func (s *appStateFacade) savePending(req *state.PendingRequest) error {
	if s == nil || s.store == nil || req == nil {
		return nil
	}
	cp := *req
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = s.frontendID
	}
	if strings.TrimSpace(cp.Backend) == "" {
		cp.Backend = s.backend
	}
	return s.store.UpsertPending(&cp)
}

func (s *appStateFacade) pendingRequests() []*state.PendingRequest {
	if s == nil || s.store == nil {
		return nil
	}
	all := s.store.AllPendingRequests()
	out := make([]*state.PendingRequest, 0, len(all))
	for _, req := range all {
		if req == nil || !s.matchesFrontend(req.FrontendID) {
			continue
		}
		out = append(out, req)
	}
	return out
}

func (s *appStateFacade) updatePending(id string, mutate func(*state.PendingRequest)) error {
	if s == nil || s.store == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	err := s.store.UpdateScopedPending(s.frontendID, id, mutate)
	if err == nil || !s.legacyFallback || s.frontendID == "" {
		return err
	}
	return s.store.UpdatePending(id, mutate)
}

func (s *appStateFacade) resolvePending(id string) *state.PendingRequest {
	_ = s.updatePending(id, func(req *state.PendingRequest) { req.Status = "resolved" })
	return s.pending(id)
}

func (s *appStateFacade) deletePendingRequests(match func(*state.PendingRequest) bool) {
	if s == nil || s.store == nil || match == nil {
		return
	}
	s.store.DeletePendingRequests(func(req *state.PendingRequest) bool {
		if req == nil || !s.matchesFrontend(req.FrontendID) {
			return false
		}
		return match(req)
	})
}
