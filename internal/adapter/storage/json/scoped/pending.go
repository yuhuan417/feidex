package appstate

import (
	"strings"

	"feidex/internal/domain/identity"
	"feidex/internal/state"
)

// Pending returns a frontend-scoped pending request by id.
func (s *Store) Pending(id string) *state.PendingRequest {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if req := s.stateStore().PendingByScopedID(s.scopeFrontendID(), id); req != nil {
		return req
	}
	if s.scopeLegacyFallback() && s.scopeFrontendID() != "" {
		return s.stateStore().PendingByID(id)
	}
	return nil
}

// SavePending persists a pending request scoped to the current frontend.
func (s *Store) SavePending(req *state.PendingRequest) error {
	if s == nil || s.stateStore() == nil || req == nil {
		return nil
	}
	cp := *req
	if strings.TrimSpace(cp.FrontendID) == "" {
		cp.FrontendID = s.scopeFrontendID()
	}
	if strings.TrimSpace(cp.Backend) == "" {
		cp.Backend = s.scopeBackend()
	}
	// Group conversations have shared permissions: pending cards and forms
	// may be completed by any real member. Keep p2p ownership restrictions.
	chatType := ""
	if sess := s.Session(cp.SessionKey); sess != nil {
		chatType = sess.ChatType
	} else {
		_, parsedChatType, chatID, _, _ := identity.ParseSessionKey(cp.SessionKey)
		chatType = parsedChatType
		if chatType == "" && chatID != "" {
			if bindings := s.stateStore().AgentBindingsByChat(s.scopeFrontendID(), "group", chatID); len(bindings) > 0 {
				chatType = "group"
			} else if primaries := s.stateStore().GroupPrimariesByChat(s.scopeFrontendID(), "group", chatID); len(primaries) > 0 {
				chatType = "group"
			}
		}
	}
	if strings.EqualFold(strings.TrimSpace(chatType), "group") {
		cp.OwnerUserID = ""
	}
	return s.stateStore().UpsertPending(&cp)
}

// PendingRequests returns all pending requests visible to this frontend.
func (s *Store) PendingRequests() []*state.PendingRequest {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	all := s.stateStore().AllPendingRequests()
	out := make([]*state.PendingRequest, 0, len(all))
	for _, req := range all {
		if req == nil || !s.matchesFrontend(req.FrontendID) {
			continue
		}
		out = append(out, req)
	}
	return out
}

// UpdatePending mutates a frontend-scoped pending request.
func (s *Store) UpdatePending(id string, mutate func(*state.PendingRequest)) error {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	err := s.stateStore().UpdateScopedPending(s.scopeFrontendID(), id, mutate)
	if err == nil || !s.scopeLegacyFallback() || s.scopeFrontendID() == "" {
		return err
	}
	return s.stateStore().UpdatePending(id, mutate)
}

// ResolvePending marks a pending request resolved and returns the snapshot.
func (s *Store) ResolvePending(id string) *state.PendingRequest {
	_ = s.UpdatePending(id, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
	return s.Pending(id)
}

// DeletePendingRequests deletes frontend-scoped pending requests matching fn.
func (s *Store) DeletePendingRequests(match func(*state.PendingRequest) bool) {
	if s == nil || s.stateStore() == nil || match == nil {
		return
	}
	s.stateStore().DeletePendingRequests(func(req *state.PendingRequest) bool {
		if req == nil || !s.matchesFrontend(req.FrontendID) {
			return false
		}
		return match(req)
	})
}
