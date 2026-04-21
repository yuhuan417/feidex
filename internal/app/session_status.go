package app

import "feidex/internal/state"

func sessionRefreshPendingStatus(sess *state.Session) {
	if sess == nil || sessionHasInFlightSubmission(sess) {
		return
	}
	if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		sess.Status = "queued"
		return
	}
	sess.Status = "idle"
}

func sessionShouldStartNextSubmissionAsync(sess *state.Session) bool {
	return sess != nil && !sessionHasInFlightSubmission(sess) && len(sess.Queue) > 0
}
