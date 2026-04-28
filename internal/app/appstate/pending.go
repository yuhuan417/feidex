package appstate

import (
	"strings"

	"feidex/internal/state"
)

// Pending returns a frontend-scoped pending request by id.
func (s *Store) Pending(id string) *state.PendingRequest {
	if s == nil || s.Store == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if req := s.Store.PendingByScopedID(s.FrontendID, id); req != nil {
		return req
	}
	if s.LegacyFallback && s.FrontendID != "" {
		return s.Store.PendingByID(id)
	}
	return nil
}

// SavePending persists a pending request scoped to the current frontend.
func (s *Store) SavePending(req *state.PendingRequest) error {
	if s == nil || s.Store == nil || req == nil {
		return nil
	}
	cp := *req
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = s.FrontendID
	}
	if strings.TrimSpace(cp.Backend) == "" {
		cp.Backend = s.Backend
	}
	return s.Store.UpsertPending(&cp)
}

// PendingRequests returns all pending requests visible to this frontend.
func (s *Store) PendingRequests() []*state.PendingRequest {
	if s == nil || s.Store == nil {
		return nil
	}
	all := s.Store.AllPendingRequests()
	out := make([]*state.PendingRequest, 0, len(all))
	for _, req := range all {
		if req == nil || !s.MatchesFrontend(req.FrontendID) {
			continue
		}
		out = append(out, req)
	}
	return out
}

// UpdatePending mutates a frontend-scoped pending request.
func (s *Store) UpdatePending(id string, mutate func(*state.PendingRequest)) error {
	if s == nil || s.Store == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	err := s.Store.UpdateScopedPending(s.FrontendID, id, mutate)
	if err == nil || !s.LegacyFallback || s.FrontendID == "" {
		return err
	}
	return s.Store.UpdatePending(id, mutate)
}

// ResolvePending marks a pending request resolved and returns the snapshot.
func (s *Store) ResolvePending(id string) *state.PendingRequest {
	_ = s.UpdatePending(id, func(req *state.PendingRequest) { req.Status = "resolved" })
	return s.Pending(id)
}

// DeletePendingRequests deletes frontend-scoped pending requests matching fn.
func (s *Store) DeletePendingRequests(match func(*state.PendingRequest) bool) {
	if s == nil || s.Store == nil || match == nil {
		return
	}
	s.Store.DeletePendingRequests(func(req *state.PendingRequest) bool {
		if req == nil || !s.MatchesFrontend(req.FrontendID) {
			return false
		}
		return match(req)
	})
}
