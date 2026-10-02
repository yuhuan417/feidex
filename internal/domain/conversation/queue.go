package conversation

import "slices"

// RefreshActiveStatus derives status after removing an active operation.
// A remaining operation keeps the session active; queued and staged input
// are considered only after all active operations have drained.
func RefreshActiveStatus(sess *Session) {
	if sess == nil {
		return
	}
	if HasActiveOperations(sess) {
		sess.Status = SessionStatusTurnStarting.String()
		for _, op := range sess.ActiveOperations {
			if op.TurnID != "" {
				sess.Status = SessionStatusTurnInProgress.String()
				break
			}
		}
		return
	}
	RefreshPendingStatus(sess)
}

// Enqueue adds an input once while preserving arrival order.
func Enqueue(sess *Session, submissionID string) {
	if sess == nil || slices.Contains(sess.Queue, submissionID) {
		return
	}
	sess.Queue = append(sess.Queue, submissionID)
}

func Dequeue(sess *Session) string {
	if sess == nil || len(sess.Queue) == 0 {
		return ""
	}
	next := sess.Queue[0]
	sess.Queue = append([]string(nil), sess.Queue[1:]...)
	return next
}

func ShouldStartNextSubmission(sess *Session) bool {
	return sess != nil && !HasInFlightSubmission(sess) && len(sess.Queue) > 0
}

// RefreshPendingStatus must not overwrite a live turn. Staged input counts
// as pending work even if no submission has been created yet.
func RefreshPendingStatus(sess *Session) {
	if sess == nil || HasInFlightSubmission(sess) {
		return
	}
	if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		sess.Status = SessionStatusQueued.String()
		return
	}
	sess.Status = SessionStatusIdle.String()
}
