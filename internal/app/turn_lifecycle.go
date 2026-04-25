package app

import (
	appturnlifecycle "feidex/internal/app/turnlifecycle"
	"feidex/internal/state"
)

// turnLifecycleService wraps the exported turnlifecycle.Service to preserve
// the lowercase method names used throughout app/.
type turnLifecycleService struct {
	inner appturnlifecycle.Service
}

func newTurnLifecycleService(app *App) *turnLifecycleService {
	return &turnLifecycleService{inner: appturnlifecycle.NewService(app)}
}

func (w *turnLifecycleService) bindPendingSubmissionTurn(threadID, turnID string, allowReview bool) bool {
	return w.inner.BindPendingSubmissionTurn(threadID, turnID, allowReview)
}

func (w *turnLifecycleService) onTurnStartedNotification(threadID, turnID string) {
	w.inner.OnTurnStartedNotification(threadID, turnID)
}

func (w *turnLifecycleService) bindPendingSubmissionForTurnCompletion(threadID, turnID string) (string, *state.Submission) {
	return w.inner.BindPendingSubmissionForTurnCompletion(threadID, turnID)
}

func (w *turnLifecycleService) finishTurn(threadID, turnID, status string) {
	w.inner.FinishTurn(threadID, turnID, status)
}
