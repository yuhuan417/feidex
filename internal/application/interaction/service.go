// Package interaction coordinates persisted human interaction transitions.
package interaction

import "feidex/internal/domain/interaction"

// Repository owns atomic mutation for one frontend. The callback only applies
// a domain transition; it never calls another service or performs transport I/O.
type Repository interface {
	UpdateRequest(id string, transition func(interaction.Request) interaction.Request) (*interaction.Request, error)
	Requests() []interaction.Request
}

type Service struct {
	Repository Repository
}

func (s Service) ReplyAccepted(id string) (*interaction.Request, error) {
	return s.Repository.UpdateRequest(id, interaction.Request.ReplyAccepted)
}

// Resolve handles the authoritative server resolution, or the equivalent
// local terminal boundary for backends that resolve requests locally.
// A duplicate resolution returns nil so callers do not resume twice.
func (s Service) Resolve(id string) (*interaction.Request, error) {
	changed := false
	request, err := s.Repository.UpdateRequest(id, func(r interaction.Request) interaction.Request {
		next, didChange := r.Resolved()
		changed = didChange
		return next
	})
	if err != nil || !changed {
		return nil, err
	}
	return request, nil
}

func (s Service) HasOpenRequest(threadID, turnID, excludeID string) bool {
	for _, request := range s.Repository.Requests() {
		if request.BlocksResume(threadID, turnID, excludeID) {
			return true
		}
	}
	return false
}
