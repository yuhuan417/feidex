package conversation

import "slices"

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
