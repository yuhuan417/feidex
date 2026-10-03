package interaction

import (
	"strings"

	"feidex/internal/domain/identity"
	domain "feidex/internal/domain/interaction"
)

type LifecycleRepository interface {
	PendingRequests() []*domain.PendingRequest
	UpdatePending(string, func(*domain.PendingRequest)) error
}

type LifecycleService struct {
	Repository   LifecycleRepository
	Frontend     string
	Presentation interface {
		ExpiredInteraction(*domain.PendingRequest, string)
	}
}

func (s LifecycleService) ExpireAndPresent(backend, key string, ids []string, reason string) {
	for _, req := range s.Expire(backend, key, ids) {
		s.Presentation.ExpiredInteraction(req, reason)
	}
}

// Expire returns only successfully committed transitions for presentation.
func (s LifecycleService) Expire(backend, sessionKey string, ids []string) []*domain.PendingRequest {
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = true
		}
	}
	var expired []*domain.PendingRequest
	for _, req := range s.Repository.PendingRequests() {
		if req == nil || !strings.EqualFold(strings.TrimSpace(req.Backend), backend) || !domain.IsPendingRequestOpen(req.Status) {
			continue
		}
		if sessionKey != "" && identity.CanonicalSessionKey(s.Frontend, req.SessionKey) != identity.CanonicalSessionKey(s.Frontend, sessionKey) {
			continue
		}
		if len(wanted) > 0 && !wanted[strings.TrimSpace(req.ID)] {
			continue
		}
		changed := false
		if err := s.Repository.UpdatePending(req.ID, func(current *domain.PendingRequest) {
			if current != nil && domain.IsPendingRequestOpen(current.Status) {
				current.Status = "expired"
				changed = true
			}
		}); err == nil && changed {
			copy := *req
			copy.Status = "expired"
			expired = append(expired, &copy)
		}
	}
	return expired
}

func (s LifecycleService) LatestTextRequest(sessionKey, userID string, kinds ...string) *domain.PendingRequest {
	allowed := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		allowed[kind] = true
	}
	var best *domain.PendingRequest
	for _, req := range s.Repository.PendingRequests() {
		if req == nil || req.Status != "pending" || !allowed[req.Kind] || identity.CanonicalSessionKey(s.Frontend, req.SessionKey) != identity.CanonicalSessionKey(s.Frontend, sessionKey) {
			continue
		}
		if req.OwnerUserID != "" && req.OwnerUserID != userID {
			continue
		}
		if best == nil || req.CreatedAt > best.CreatedAt {
			best = req
		}
	}
	return best
}
