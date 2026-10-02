package submission

import (
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"strings"
)

// FindSubmissionByTurn finds the submission associated with a given turn or
// thread. Returns the session key and submission, or ("", nil) if not found.
func (s SubmissionQueueService) FindSubmissionByTurn(threadID, turnID string) (string, *domainsubmission.Submission) {
	a := s.Deps
	appState := a.AppState
	runtimeState := a.RuntimeState

	if strings.TrimSpace(turnID) != "" {
		if sessionKey, sub := runtimeState.BoundSubmissionForTurn(turnID); sub != nil {
			return sessionKey, sub
		}
		for _, sess := range appState.Sessions() {
			if sess == nil {
				continue
			}
			op := conversation.FindActiveOperationByTurn(sess, turnID)
			if op == nil || strings.TrimSpace(op.SubmissionID) == "" {
				continue
			}
			sub := appState.Submission(op.SubmissionID)
			if sub != nil {
				return sess.Key, sub
			}
		}
		return "", nil
	}
	if strings.TrimSpace(threadID) != "" {
		for _, sess := range appState.Sessions() {
			if sess == nil {
				continue
			}
			op := conversation.FindActiveOperationByThread(sess, threadID)
			if op == nil || strings.TrimSpace(op.SubmissionID) == "" {
				continue
			}
			sub := appState.Submission(op.SubmissionID)
			if sub != nil {
				return sess.Key, sub
			}
		}
	}
	return "", nil
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
