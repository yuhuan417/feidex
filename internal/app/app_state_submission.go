package app

import (
	"strings"

	"feidex/internal/state"
)

func (s *appStateFacade) createSubmission(sub *state.Submission) (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	return s.store.CreateSubmission(sub)
}

func (s *appStateFacade) deleteSubmission(id string) {
	if s == nil || s.store == nil {
		return
	}
	s.store.DeleteSubmission(strings.TrimSpace(id))
}

func (s *appStateFacade) submission(id string) *state.Submission {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.GetSubmission(strings.TrimSpace(id))
}

func (s *appStateFacade) updateSubmission(id string, mutate func(*state.Submission)) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.UpdateSubmission(strings.TrimSpace(id), mutate)
}

func (s *appStateFacade) setSubmissionStatus(id, status string) error {
	return s.updateSubmission(id, func(sub *state.Submission) {
		sub.Status = strings.TrimSpace(status)
	})
}

func (s *appStateFacade) markSubmissionRunning(id, threadID, turnID string) error {
	return s.updateSubmission(id, func(sub *state.Submission) {
		sub.ThreadID = strings.TrimSpace(threadID)
		if strings.TrimSpace(turnID) != "" {
			sub.TurnID = strings.TrimSpace(turnID)
		}
		sub.Status = "running"
	})
}

func (s *appStateFacade) finalizeSubmission(id, status string) error {
	return s.updateSubmission(id, func(sub *state.Submission) {
		sub.Status = strings.TrimSpace(status)
		sub.Finalized = true
	})
}

func (s *appStateFacade) queueSubmission(sessionKey, submissionID string) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.QueueSubmission(strings.TrimSpace(sessionKey), strings.TrimSpace(submissionID))
}

func (s *appStateFacade) dequeueSubmission(sessionKey string) (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	return s.store.DequeueSubmission(strings.TrimSpace(sessionKey))
}
