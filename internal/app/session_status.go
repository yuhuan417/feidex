package app

import "feidex/internal/state"

func sessionRefreshPendingStatus(sess *state.Session) {
	if sess == nil || sessionHasInFlightSubmission(sess) {
		return
	}
	if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		sess.Status = state.SessionStatusQueued.String()
		return
	}
	sess.Status = state.SessionStatusIdle.String()
}

// sessionShouldStartNextSubmissionAsync delegates to
// submission.ShouldStartNextSubmissionAsync.
