package appstate

import (
	domainsubmission "feidex/internal/domain/submission"
	"strings"
)

// CreateSubmission stores a new submission.
func (s *Store) CreateSubmission(sub *domainsubmission.Submission) (string, error) {
	if s == nil || s.stateStore() == nil {
		return "", nil
	}
	return s.stateStore().CreateSubmission(sub)
}

// DeleteSubmission removes a submission by id.
func (s *Store) DeleteSubmission(id string) {
	if s == nil || s.stateStore() == nil {
		return
	}
	s.stateStore().DeleteSubmission(strings.TrimSpace(id))
}

// Submission returns a submission by id.
func (s *Store) Submission(id string) *domainsubmission.Submission {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().GetSubmission(strings.TrimSpace(id))
}

// UpdateSubmission mutates an existing submission.
func (s *Store) UpdateSubmission(id string, mutate func(*domainsubmission.Submission)) error {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().UpdateSubmission(strings.TrimSpace(id), mutate)
}

// SetSubmissionStatus updates a submission status.
func (s *Store) SetSubmissionStatus(id, status string) error {
	return s.UpdateSubmission(id, func(sub *domainsubmission.Submission) {
		sub.SetStatus(status)
	})
}

// MarkSubmissionRunning records the thread/turn for a running submission.
func (s *Store) MarkSubmissionRunning(id, threadID, turnID string) error {
	return s.UpdateSubmission(id, func(sub *domainsubmission.Submission) {
		sub.MarkRunning(threadID, turnID)
	})
}

// FinalizeSubmission marks a submission terminal.
func (s *Store) FinalizeSubmission(id, status string) error {
	return s.UpdateSubmission(id, func(sub *domainsubmission.Submission) {
		sub.Finalize(status)
	})
}

// QueueSubmission appends a submission to a session queue.
func (s *Store) QueueSubmission(sessionKey, submissionID string) error {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().QueueSubmission(strings.TrimSpace(sessionKey), strings.TrimSpace(submissionID))
}

// DequeueSubmission pops the next queued submission.
func (s *Store) DequeueSubmission(sessionKey string) (string, error) {
	if s == nil || s.stateStore() == nil {
		return "", nil
	}
	return s.stateStore().DequeueSubmission(strings.TrimSpace(sessionKey))
}
