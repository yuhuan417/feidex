package app

import (
	"context"

	appsubmission "feidex/internal/app/submission"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (w *submissionCoordinator) enqueueSubmissionWithSessionKey(msg *feishu.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
	return newSubmissionQueueServiceFromApp(w.app).EnqueueSubmission(msg, sessionKey, bindOnlyCurrentRoot)
}

func (w *submissionCoordinator) notifySubmissionStartFailure(ctx context.Context, sub *state.Submission, err error, willContinue bool) {
	newSubmissionQueueServiceFromApp(w.app).NotifySubmissionStartFailure(ctx, sub, err, willContinue)
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

func sessionShouldStartNextSubmissionAsync(sess *state.Session) bool {
	return appsubmission.ShouldStartNextSubmissionAsync(sess)
}

// submissionDispatchAdapter implements appturnlifecycle.SubmissionDispatchProvider
// by wrapping the submission.SubmissionQueueService.
type submissionDispatchAdapter struct{ app *App }

func (a submissionDispatchAdapter) StartNextSubmissionAsync(sessionKey, source string) {
	newSubmissionQueueServiceFromApp(a.app).StartNextSubmissionAsync(sessionKey, source)
}
