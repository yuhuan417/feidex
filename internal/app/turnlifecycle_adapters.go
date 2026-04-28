package app

import (
	"context"

	appturnlifecycle "feidex/internal/app/turnlifecycle"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// Provider adapters — satisfy turnlifecycle narrow interfaces
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// *App methods satisfying turnlifecycle.App
// ---------------------------------------------------------------------------

func (a *App) TurnLifecycleAppState() appturnlifecycle.AppStateProvider {
	return a.State()
}

func (a *App) TurnLifecycleRuntimeState() appturnlifecycle.RuntimeStateProvider {
	return newRuntimeStateService(a)
}

func (a *App) TurnLifecycleReplyContinuation() appturnlifecycle.ReplyContinuationProvider {
	return newReplyContinuationService(a)
}

func (a *App) TurnLifecycleTurnStream() appturnlifecycle.TurnStreamProvider {
	return newTurnStreamService(a)
}

func (a *App) TurnLifecyclePendingQueue() appturnlifecycle.PendingQueueProvider {
	return newPendingQueueService(a)
}

func (a *App) TurnLifecycleOutboundCard() appturnlifecycle.OutboundCardProvider {
	return newOutboundCardService(a)
}

func (a *App) TurnLifecycleSubmissionDispatch() appturnlifecycle.SubmissionDispatchProvider {
	return newSubmissionCoordinator(a)
}

func (a *App) TurnLifecycleAutoRetry() appturnlifecycle.AutoRetryProvider {
	return newAutoRetryService(a)
}

func (a *App) TurnLifecycleRuntimeMaintenance() appturnlifecycle.RuntimeMaintenanceProvider {
	return newRuntimeMaintenanceService(a)
}

func (a *App) MarkSessionThreadLive(sessionKey, threadID string) {
	markSessionThreadLive(a, sessionKey, threadID)
}

func (a *App) TurnStopAttentionUserID(sub *state.Submission, turnID string) string {
	return turnStopAttentionUserID(a, sub, turnID)
}

func (a *App) SendEmptyFinalCardWithReuse(ctx context.Context, sub *state.Submission, footerLines []string, reuseMessageID string) string {
	return sendEmptyFinalCardWithReuse(a, ctx, sub, footerLines, reuseMessageID)
}

func (a *App) SessionShouldStartNextSubmissionAsync(sess *state.Session) bool {
	return sessionShouldStartNextSubmissionAsync(sess)
}

func (a *App) BindStandaloneCompactTurn(threadID, turnID string) bool {
	return bindStandaloneCompactTurn(a, threadID, turnID)
}

func (a *App) FinishStandaloneCompactTurn(threadID, turnID, status string) bool {
	return finishStandaloneCompactTurn(a, threadID, turnID, status)
}

func (a *App) FindSubmissionByTurn(threadID, turnID string) (string, *state.Submission) {
	return findSubmissionByTurn(a, threadID, turnID)
}

func (a *App) LogSessionState(event, sessionKey string, sess *state.Session) {
	logSessionState(event, sessionKey, sess)
}
