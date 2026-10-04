package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/turn"
	"feidex/internal/adapter/feishu/turnitem"
	appturn "feidex/internal/application/turn"
	"feidex/internal/config"
	domainsubmission "feidex/internal/domain/submission"

	appturnstream "feidex/internal/adapter/feishu/turnstream"
)

// ---------------------------------------------------------------------------
// Provider adapters — satisfy turnstream narrow interfaces
// ---------------------------------------------------------------------------

type turnStreamOutboundCardAdapter struct {
	app     *App
	compact interface {
		CompleteStandaloneCompactItem(string, string, map[string]any) bool
	}
}

func (a turnStreamOutboundCardAdapter) SendPlanCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, planText, reuseMessageID string) string {
	return newOutboundCardService(a.app).sendPlanCardWithReuse(ctx, sub, planText, reuseMessageID)
}
func (a turnStreamOutboundCardAdapter) SendTurnItemCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, payload turnitem.CardPayload, reuseMessageID string) string {
	return newOutboundCardService(a.app).sendTurnItemCardWithReuse(ctx, sub, payload, reuseMessageID)
}
func (a turnStreamOutboundCardAdapter) CompleteStandaloneCompactItem(threadID, turnID string, item turnitem.ProtocolItem) bool {
	return a.compact.CompleteStandaloneCompactItem(threadID, turnID, item.MergedRaw())
}

type turnStreamQuietCardExecutorAdapter struct{ app *App }

func (a turnStreamQuietCardExecutorAdapter) ExecuteQuietWorkingCardOp(ctx context.Context, sub *domainsubmission.Submission, op turn.QuietWorkingCardOp) {
	executeQuietWorkingCardOp(a.app, ctx, sub, op)
}

type claudeTurnStreamPort struct{ app *App }

func (p claudeTurnStreamPort) NoteTurnItemStarted(threadID, turnID string, item turnitem.ProtocolItem) {
	p.app.bindings.ItemContext.Start(threadID, turnID, item)
}
func (p claudeTurnStreamPort) UpdateInFlightTurnItem(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
	p.app.bindings.TurnPresentation.UpdateInFlightTurnItem(ctx, threadID, turnID, itemID, item)
}
func (p claudeTurnStreamPort) RecordTurnError(threadID, turnID, message string) {
	p.app.bindings.TurnPresentation.RecordTurnError(threadID, turnID, message)
}
func (p claudeTurnStreamPort) CompleteTurnItem(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
	p.app.bindings.TurnPresentation.CompleteTurnItem(ctx, threadID, turnID, itemID, item)
}
func (p claudeTurnStreamPort) PrepareTurnStreamQuietBoundary(turnID string) string {
	return p.app.bindings.TurnPresentation.PrepareStreamQuietBoundary(turnID).ReuseMessageID
}
func (p claudeTurnStreamPort) PrepareTurnStreamQuietUpdate(sessionKey string, sub *domainsubmission.Submission, threadID, itemID string, item turnitem.ProtocolItem, workspaceCwd string) turn.QuietWorkingCardOp {
	return p.app.bindings.TurnPresentation.PrepareStreamQuietUpdate(sessionKey, sub, threadID, itemID, item, workspaceCwd)
}
func (p claudeTurnStreamPort) MarkTurnStreamFinal(turnID string) {
	p.app.bindings.TurnPresentation.MarkStreamFinal(turnID)
}

func TurnPresentationPorts(a *App, turns *appturn.Service) appturnstream.Dependencies {
	return appturnstream.Dependencies{
		Context: a.Context,
		Tracker: a.bindings.TurnStreams, Finder: a.bindings.SubmissionLookup, Lifecycle: turns, Runtime: turnItemsPort{tracker: a.bindings.TurnItems},
		Outbound: turnStreamOutboundCardAdapter{app: a, compact: a.bindings.Compaction}, Quiet: turnStreamQuietCardExecutorAdapter{app: a},
		SendStartedNotice: func(ctx context.Context, sub *domainsubmission.Submission) {
			maybeSendSubmissionStartedNotice(a, ctx, sub)
		},
		WorkspaceCwd: func(id string) string {
			if ws := config.FindWorkspace(a.cfg, id); ws != nil {
				return ws.Cwd
			}
			return ""
		},
		FeishuConfig: func() *config.FeishuConfig { return a.configView().feishuConfig() },
	}
}

type turnItemsPort struct{ tracker *turnitem.Tracker }

func (p turnItemsPort) CompleteTurnItemState(threadID, turnID, itemID string, item turnitem.ProtocolItem) turnitem.ProtocolItem {
	return p.tracker.Complete(threadID, turnID, itemID, item)
}

func (p turnItemsPort) ClearTurnItemStates(turnID string) { p.tracker.ClearTurn(turnID) }
