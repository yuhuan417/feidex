package turn

import (
	"strings"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/submission"
	"feidex/internal/domain/turn"
)

// FinishSteerSubmission closes a steer input after its owning backend round ends.
func (s Service) FinishSteerSubmission(id, status string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	st := s.deps.State
	sub := st.Submission(id)
	if sub == nil || sub.Finalized {
		return
	}
	terminal := turn.CompletionStatus(status).String()
	if terminal != submission.SubmissionStatusCompleted.String() && terminal != submission.SubmissionStatusInterrupted.String() {
		terminal = submission.SubmissionStatusFailed.String()
	}
	if err := st.FinalizeSubmission(id, terminal); err != nil {
		return
	}
	s.deps.Reactions.ClearSubmissionProcessingReactions(sub)
	if key := strings.TrimSpace(sub.SessionKey); key != "" {
		_, _ = st.UpdateSession(key, func(sess *conversation.Session) {
			if sess != nil {
				conversation.RemoveActiveOperation(sess, id, strings.TrimSpace(sub.TurnID))
				if !conversation.HasActiveOperations(sess) {
					sess.Status = conversation.SessionStatusIdle.String()
				}
			}
		})
	}
}
