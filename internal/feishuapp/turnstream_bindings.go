package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/turn"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/config"
	domainsubmission "feidex/internal/domain/submission"

	appturnstream "feidex/internal/adapter/feishu/turnstream"
	appsubmission "feidex/internal/application/submission"
)

// ---------------------------------------------------------------------------
// Provider adapters — satisfy turnstream narrow interfaces
// ---------------------------------------------------------------------------

type turnStreamSubmissionFinderAdapter struct{ app *App }

func (a turnStreamSubmissionFinderAdapter) FindSubmissionByTurn(threadID, turnID string) (string, *domainsubmission.Submission) {
	return (appsubmission.SubmissionLookupService{State: a.app.State(), Runtime: newRuntimeStateService(a.app)}).FindSubmissionByTurn(threadID, turnID)
}

type turnStreamTurnLifecycleAdapter struct{ app *App }

func (a turnStreamTurnLifecycleAdapter) BindPendingSubmissionTurn(threadID, turnID string, allowReview bool) bool {
	return newTurnLifecycleService(a.app).BindPendingSubmissionTurn(threadID, turnID, allowReview)
}

type turnStreamOutboundCardAdapter struct{ app *App }

func (a turnStreamOutboundCardAdapter) SendPlanCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, planText, reuseMessageID string) string {
	return newOutboundCardService(a.app).sendPlanCardWithReuse(ctx, sub, planText, reuseMessageID)
}
func (a turnStreamOutboundCardAdapter) SendTurnItemCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, payload turnitem.CardPayload, reuseMessageID string) string {
	return newOutboundCardService(a.app).sendTurnItemCardWithReuse(ctx, sub, payload, reuseMessageID)
}
func (a turnStreamOutboundCardAdapter) CompleteStandaloneCompactItem(threadID, turnID string, item turnitem.ProtocolItem) bool {
	return newCompactionService(a.app).CompleteStandaloneCompactItem(threadID, turnID, item.MergedRaw())
}

type turnStreamQuietCardExecutorAdapter struct{ app *App }

func (a turnStreamQuietCardExecutorAdapter) ExecuteQuietWorkingCardOp(ctx context.Context, sub *domainsubmission.Submission, op turn.QuietWorkingCardOp) {
	executeQuietWorkingCardOp(a.app, ctx, sub, op)
}

type claudeTurnStreamPort struct{ app *App }

func (p claudeTurnStreamPort) NoteTurnItemStarted(threadID, turnID string, item turnitem.ProtocolItem) {
	newRuntimeStateService(p.app).noteTurnItemStartedPayload(threadID, turnID, item)
}
func (p claudeTurnStreamPort) UpdateInFlightTurnItem(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
	newTurnStreamService(p.app).updateInFlightTurnItemPayload(ctx, threadID, turnID, itemID, item)
}
func (p claudeTurnStreamPort) RecordTurnError(threadID, turnID, message string) {
	newTurnStreamService(p.app).recordTurnError(threadID, turnID, message)
}
func (p claudeTurnStreamPort) CompleteTurnItem(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
	newTurnStreamService(p.app).completeTurnItemPayload(ctx, threadID, turnID, itemID, item)
}
func (p claudeTurnStreamPort) PrepareTurnStreamQuietBoundary(turnID string) string {
	return newTurnStreamService(p.app).prepareTurnStreamQuietBoundary(turnID).ReuseMessageID
}
func (p claudeTurnStreamPort) PrepareTurnStreamQuietUpdate(sessionKey string, sub *domainsubmission.Submission, threadID, itemID string, item turnitem.ProtocolItem, workspaceCwd string) turn.QuietWorkingCardOp {
	return newTurnStreamService(p.app).prepareTurnStreamQuietUpdatePayload(sessionKey, sub, threadID, itemID, item, workspaceCwd)
}
func (p claudeTurnStreamPort) MarkTurnStreamFinal(turnID string) {
	newTurnStreamService(p.app).markTurnStreamFinal(turnID)
}

func newTurnPresentation(a *App) appturnstream.Service {
	trackers := a.Trackers()
	if trackers.turnStreams == nil {
		trackers.turnStreams = appturnstream.NewTracker()
	}
	return appturnstream.NewService(appturnstream.Dependencies{
		Tracker: trackers.turnStreams, Finder: turnStreamSubmissionFinderAdapter{app: a}, Lifecycle: turnStreamTurnLifecycleAdapter{app: a}, Runtime: newRuntimeStateService(a),
		Outbound: turnStreamOutboundCardAdapter{app: a}, Quiet: turnStreamQuietCardExecutorAdapter{app: a},
		SendStartedNotice: func(ctx context.Context, sub *domainsubmission.Submission) { sendSubmissionStartedNotice(a, ctx, sub) },
		WorkspaceCwd: func(id string) string {
			if ws := config.FindWorkspace(a.cfg, id); ws != nil {
				return ws.Cwd
			}
			return ""
		},
		FeishuConfig: func() *config.FeishuConfig { return feishuConfig(a) },
	})
}
