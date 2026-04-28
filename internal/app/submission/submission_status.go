package submission

import (
	"strings"

	"feidex/internal/app/sessionctx"
	"feidex/internal/state"
)

// FindSubmissionByTurn finds the submission associated with a given turn or
// thread. Returns the session key and submission, or ("", nil) if not found.
func (s SubmissionQueueService) FindSubmissionByTurn(threadID, turnID string) (string, *state.Submission) {
	a := s.App
	appState := a.SubmissionQueueAppState()
	runtimeState := a.SubmissionQueueRuntimeState()

	if strings.TrimSpace(turnID) != "" {
		if sessionKey, sub := runtimeState.BoundSubmissionForTurn(turnID); sub != nil {
			return sessionKey, sub
		}
		for _, sess := range appState.Sessions() {
			if sess == nil {
				continue
			}
			op := sessionctx.FindActiveOperationByTurn(sess, turnID)
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
			op := sessionctx.FindActiveOperationByThread(sess, threadID)
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
func (s SubmissionQueueService) UpdateSubmissionByTurn(threadID, turnID string, mutate func(*state.Submission)) {
	appState := s.App.SubmissionQueueAppState()
	_, sub := s.FindSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	_ = appState.UpdateSubmission(sub.ID, mutate)
}
