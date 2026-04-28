package appstate

import (
	"strings"

	"feidex/internal/state"
)

// CreateSubmission stores a new submission.
func (s *Store) CreateSubmission(sub *state.Submission) (string, error) {
	if s == nil || s.Store == nil {
		return "", nil
	}
	return s.Store.CreateSubmission(sub)
}

// DeleteSubmission removes a submission by id.
func (s *Store) DeleteSubmission(id string) {
	if s == nil || s.Store == nil {
		return
	}
	s.Store.DeleteSubmission(strings.TrimSpace(id))
}

// Submission returns a submission by id.
func (s *Store) Submission(id string) *state.Submission {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.GetSubmission(strings.TrimSpace(id))
}

// UpdateSubmission mutates an existing submission.
func (s *Store) UpdateSubmission(id string, mutate func(*state.Submission)) error {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.UpdateSubmission(strings.TrimSpace(id), mutate)
}

// SetSubmissionStatus updates a submission status.
func (s *Store) SetSubmissionStatus(id, status string) error {
	return s.UpdateSubmission(id, func(sub *state.Submission) {
		sub.Status = state.NormalizeSubmissionStatus(status).String()
	})
}

// MarkSubmissionRunning records the thread/turn for a running submission.
func (s *Store) MarkSubmissionRunning(id, threadID, turnID string) error {
	return s.UpdateSubmission(id, func(sub *state.Submission) {
		sub.ThreadID = strings.TrimSpace(threadID)
		if strings.TrimSpace(turnID) != "" {
			sub.TurnID = strings.TrimSpace(turnID)
		}
		sub.Status = state.SubmissionStatusRunning.String()
	})
}

// FinalizeSubmission marks a submission terminal.
func (s *Store) FinalizeSubmission(id, status string) error {
	return s.UpdateSubmission(id, func(sub *state.Submission) {
		sub.Status = state.NormalizeSubmissionStatus(status).String()
		sub.Finalized = true
	})
}

// QueueSubmission appends a submission to a session queue.
func (s *Store) QueueSubmission(sessionKey, submissionID string) error {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.QueueSubmission(strings.TrimSpace(sessionKey), strings.TrimSpace(submissionID))
}

// DequeueSubmission pops the next queued submission.
func (s *Store) DequeueSubmission(sessionKey string) (string, error) {
	if s == nil || s.Store == nil {
		return "", nil
	}
	return s.Store.DequeueSubmission(strings.TrimSpace(sessionKey))
}
