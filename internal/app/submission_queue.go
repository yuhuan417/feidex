package app

import (
	"context"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (w *submissionCoordinator) enqueueSubmissionWithSessionKey(msg *feishu.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
	return newSubmissionQueueServiceFromApp(w.app).EnqueueSubmission(msg, sessionKey, bindOnlyCurrentRoot)
}

func (w *submissionCoordinator) startNextSubmission(sessionKey string) error {
	return newSubmissionQueueServiceFromApp(w.app).StartNextSubmission(sessionKey)
}

func (w *submissionCoordinator) notifySubmissionStartFailure(ctx context.Context, sub *state.Submission, err error, willContinue bool) {
	newSubmissionQueueServiceFromApp(w.app).NotifySubmissionStartFailure(ctx, sub, err, willContinue)
}

func (w *submissionCoordinator) handleSubmissionStartFailure(sessionKey, threadID string, sub *state.Submission, err error, notifyFailure bool) {
	newSubmissionQueueServiceFromApp(w.app).HandleSubmissionStartFailure(sessionKey, threadID, sub, err, notifyFailure)
}

func shouldDropCodexThreadLineageAfterStartFailure(a *App, err error) bool {
	if a == nil || err == nil {
		return false
	}
	if runtime := backendRuntime(a); runtime != nil {
		return runtime.dropThreadLineageAfterStartFailure(a, err)
	}
	return false
}

func (w *submissionCoordinator) startNextSubmissionWithFailureNotice(sessionKey string, notifyFailure bool) error {
	return newSubmissionQueueServiceFromApp(w.app).StartNextSubmissionWithFailureNotice(sessionKey, notifyFailure)
}

func (w *submissionCoordinator) startNextCodexSubmissionWithFailureNotice(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	return newSubmissionQueueServiceFromApp(w.app).StartNextCodexSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
}

// Exported wrapper for sub-package interface satisfaction.
func (w *submissionCoordinator) StartNextSubmissionAsync(sessionKey, source string) {
	w.startNextSubmissionAsync(sessionKey, source)
}

func (w *submissionCoordinator) startNextSubmissionAsync(sessionKey, source string) {
	newSubmissionQueueServiceFromApp(w.app).StartNextSubmissionAsync(sessionKey, source)
}

func sessionShouldStartNextSubmissionAsync(sess *state.Session) bool {
	if sess == nil {
		return false
	}
	return !sessionHasInFlightSubmission(sess) && len(sess.Queue) > 0
}
