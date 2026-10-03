// Package interaction coordinates persisted human interaction transitions.
package interaction

import (
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/submission"
	"strings"
)

// Repository owns atomic mutation for one frontend. The callback only applies
// a domain transition; it never calls another service or performs transport I/O.
type Repository interface {
	UpdateRequest(id string, transition func(interaction.Request) interaction.Request) (*interaction.Request, error)
	Requests() []interaction.Request
}

type SubmissionResumer interface {
	FindSubmissionByTurn(string, string) (string, *submission.Submission)
	SetSubmissionStatus(string, string) error
}

type Dependencies struct {
	Repository  Repository
	Submissions SubmissionResumer
}
type Service struct{ Deps Dependencies }

func (s Service) ReplyAccepted(id string) (*interaction.Request, error) {
	return s.Deps.Repository.UpdateRequest(id, interaction.Request.ReplyAccepted)
}

func (s Service) FinalizeReply(id string) (*interaction.Request, error) {
	var local bool
	var changed bool
	request, err := s.Deps.Repository.UpdateRequest(id, func(r interaction.Request) interaction.Request {
		local = strings.EqualFold(strings.TrimSpace(r.Backend), "claude") || !interaction.IsServerResolvedPendingKind(r.Kind)
		if local {
			next, didChange := r.Resolved()
			changed = didChange
			return next
		}
		return r.ReplyAccepted()
	})
	if err != nil || !local || !changed {
		return request, err
	}
	return request, s.ResumeSubmission(request)
}

// Resolve handles the authoritative server resolution, or the equivalent
// local terminal boundary for backends that resolve requests locally.
// A duplicate resolution returns nil so callers do not resume twice.
func (s Service) Resolve(id string) (*interaction.Request, error) {
	changed := false
	request, err := s.Deps.Repository.UpdateRequest(id, func(r interaction.Request) interaction.Request {
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
	for _, request := range s.Deps.Repository.Requests() {
		if request.BlocksResume(threadID, turnID, excludeID) {
			return true
		}
	}
	return false
}

// ResumeSubmission runs only after the authoritative interaction transition.
// Another pending or replied request for the turn keeps the submission paused.
func (s Service) ResumeSubmission(request *interaction.Request) error {
	if request == nil || s.Deps.Submissions == nil || s.HasOpenRequest(request.ThreadID, request.TurnID, request.ID) {
		return nil
	}
	_, sub := s.Deps.Submissions.FindSubmissionByTurn(request.ThreadID, request.TurnID)
	if sub == nil {
		return nil
	}
	return s.Deps.Submissions.SetSubmissionStatus(sub.ID, submission.SubmissionStatusRunning.String())
}

func (s Service) ResolveAndResume(id string) (*interaction.Request, error) {
	request, err := s.Resolve(id)
	if err != nil {
		return nil, err
	}
	return request, s.ResumeSubmission(request)
}
