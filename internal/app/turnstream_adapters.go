package app

import (
	"context"

	appturnstream "feidex/internal/app/turnstream"
	"feidex/internal/app/turnitem"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// Provider adapters — satisfy turnstream narrow interfaces
// ---------------------------------------------------------------------------

type turnStreamSubmissionFinderAdapter struct{ app *App }

func (a turnStreamSubmissionFinderAdapter) FindSubmissionByTurn(threadID, turnID string) (string, *state.Submission) {
	return findSubmissionByTurn(a.app, threadID, turnID)
}

type turnStreamTurnLifecycleAdapter struct{ app *App }

func (a turnStreamTurnLifecycleAdapter) BindPendingSubmissionTurn(threadID, turnID string, allowReview bool) bool {
	return newTurnLifecycleService(a.app).bindPendingSubmissionTurn(threadID, turnID, allowReview)
}

type turnStreamOutboundCardAdapter struct{ app *App }

func (a turnStreamOutboundCardAdapter) SendPlanCardWithReuse(ctx context.Context, sub *state.Submission, planText, reuseMessageID string) string {
	return newOutboundCardService(a.app).sendPlanCardWithReuse(ctx, sub, planText, reuseMessageID)
}
func (a turnStreamOutboundCardAdapter) SendTurnItemCardWithReuse(ctx context.Context, sub *state.Submission, payload turnitem.CardPayload, reuseMessageID string) string {
	return newOutboundCardService(a.app).sendTurnItemCardWithReuse(ctx, sub, payload, reuseMessageID)
}
func (a turnStreamOutboundCardAdapter) CompleteStandaloneCompactItem(threadID, turnID string, item map[string]any) bool {
	return completeStandaloneCompactItem(a.app, threadID, turnID, item)
}

type turnStreamQuietCardExecutorAdapter struct{ app *App }

func (a turnStreamQuietCardExecutorAdapter) ExecuteQuietWorkingCardOp(ctx context.Context, sub *state.Submission, op quietWorkingCardOp) {
	executeQuietWorkingCardOp(a.app, ctx, sub, op)
}

// ---------------------------------------------------------------------------
// *App methods satisfying turnstream.App
// ---------------------------------------------------------------------------

// TurnStreamState returns the narrowed state provider for the turn stream service.
func (a *App) TurnStreamState() appturnstream.StateProvider {
	if a == nil {
		return nil
	}
	return appState(a)
}

// TurnStreamSubmissionFinder returns the narrowed submission finder for the turn stream service.
func (a *App) TurnStreamSubmissionFinder() appturnstream.SubmissionFinderProvider {
	if a == nil {
		return nil
	}
	return turnStreamSubmissionFinderAdapter{app: a}
}

// TurnStreamTurnLifecycle returns the narrowed turn lifecycle provider for the turn stream service.
func (a *App) TurnStreamTurnLifecycle() appturnstream.TurnLifecycleProvider {
	return turnStreamTurnLifecycleAdapter{app: a}
}

// TurnStreamRuntimeState returns the narrowed runtime state provider for the turn stream service.
func (a *App) TurnStreamRuntimeState() appturnstream.RuntimeStateProvider {
	return newRuntimeStateService(a)
}

// TurnStreamOutboundCards returns the narrowed outbound card provider for the turn stream service.
func (a *App) TurnStreamOutboundCards() appturnstream.OutboundCardProvider {
	return turnStreamOutboundCardAdapter{app: a}
}

// TurnStreamQuietCardExecutor returns the quiet card executor for the turn stream service.
func (a *App) TurnStreamQuietCardExecutor() appturnstream.QuietCardExecutorProvider {
	return turnStreamQuietCardExecutorAdapter{app: a}
}

// SendSubmissionStartedNotice sends the "turn started" notice for a submission.
func (a *App) SendSubmissionStartedNotice(ctx context.Context, sub *state.Submission) {
	sendSubmissionStartedNotice(a, ctx, sub)
}

// TurnStreamTracker returns the turn stream tracker, lazily initializing it.
func (a *App) TurnStreamTracker() *appturnstream.Tracker {
	if a == nil {
		return nil
	}
	if a.trackers.turnStreams == nil {
		a.trackers.turnStreams = appturnstream.NewTracker()
	}
	return a.trackers.turnStreams
}
