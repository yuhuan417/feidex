package submission

import (
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"strings"
)

// SubmissionLookupService is the read-only owner for turn -> submission
// lookup. Keeping this separate from queue orchestration prevents turn,
// interaction and streaming adapters from recursively constructing the queue
// coordinator just to inspect state.
type SubmissionLookupService struct {
	State   QueueStateProvider
	Runtime interface {
		BoundSubmissionForTurn(string) (string, *domainsubmission.Submission)
	}
}

type StatusRepository interface {
	UpdateSubmission(string, func(*domainsubmission.Submission)) error
}

// ClaimStartNotice atomically claims the queued-input notice before presentation.
func (s StatusService) ClaimStartNotice(id string) (bool, error) {
	claimed := false
	err := s.Repository.UpdateSubmission(id, func(current *domainsubmission.Submission) {
		if current != nil && current.WaitedInQueue && !current.StartNoticeSent {
			current.StartNoticeSent = true
			claimed = true
		}
	})
	return claimed, err
}

type StatusService struct {
	Lookup     SubmissionLookupService
	Repository StatusRepository
}

// RecordTurnFailure records diagnostics without finalizing the turn. The
// turn lifecycle owner still waits for the backend's completion notification.
func (s StatusService) RecordTurnFailure(threadID, turnID string) error {
	_, sub := s.Lookup.FindSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return nil
	}
	return s.Repository.UpdateSubmission(sub.ID, func(current *domainsubmission.Submission) {
		current.Status = domainsubmission.SubmissionStatusFailed.String()
	})
}

func (s SubmissionLookupService) FindSubmissionByTurn(threadID, turnID string) (string, *domainsubmission.Submission) {
	if s.State == nil || s.Runtime == nil {
		return "", nil
	}
	if strings.TrimSpace(turnID) != "" {
		if sessionKey, sub := s.Runtime.BoundSubmissionForTurn(turnID); sub != nil {
			return sessionKey, sub
		}
		for _, sess := range s.State.Sessions() {
			if sess == nil {
				continue
			}
			op := conversation.FindActiveOperationByTurn(sess, turnID)
			if op == nil || strings.TrimSpace(op.SubmissionID) == "" {
				continue
			}
			if sub := s.State.Submission(op.SubmissionID); sub != nil {
				return sess.Key, sub
			}
		}
		return "", nil
	}
	if strings.TrimSpace(threadID) == "" {
		return "", nil
	}
	for _, sess := range s.State.Sessions() {
		if sess == nil {
			continue
		}
		op := conversation.FindActiveOperationByThread(sess, threadID)
		if op == nil || strings.TrimSpace(op.SubmissionID) == "" {
			continue
		}
		if sub := s.State.Submission(op.SubmissionID); sub != nil {
			return sess.Key, sub
		}
	}
	return "", nil
}

// FindSubmissionByTurn finds the submission associated with a given turn or
// thread. Returns the session key and submission, or ("", nil) if not found.
func (s SubmissionQueueService) FindSubmissionByTurn(threadID, turnID string) (string, *domainsubmission.Submission) {
	return (SubmissionLookupService{State: s.Deps.AppState, Runtime: s.Deps.RuntimeState}).FindSubmissionByTurn(threadID, turnID)
}

// UpdateSubmissionByTurn finds the submission for the given turn/thread and
// applies the mutation. No-op if the submission is not found.
func (s SubmissionQueueService) UpdateSubmissionByTurn(threadID, turnID string, mutate func(*domainsubmission.Submission)) {
	appState := s.Deps.AppState
	_, sub := s.FindSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	_ = appState.UpdateSubmission(sub.ID, mutate)
}
