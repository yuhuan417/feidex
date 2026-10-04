package feishuapp

import (
	"context"

	appapproval "feidex/internal/adapter/feishu/approval"
	"feidex/internal/adapter/feishu/turn"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/application/compaction"
	appsubmission "feidex/internal/application/submission"
	appturn "feidex/internal/application/turn"
	"feidex/internal/config"
	domainsubmission "feidex/internal/domain/submission"

	appturnstream "feidex/internal/adapter/feishu/turnstream"
)

// ---------------------------------------------------------------------------
// Provider adapters — satisfy turnstream narrow interfaces
// ---------------------------------------------------------------------------

type turnStreamOutboundCardAdapter struct {
	cards   OutboundCardService
	compact interface {
		CompleteStandaloneCompactItem(string, string, map[string]any) bool
	}
}

func (a turnStreamOutboundCardAdapter) SendPlanCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, planText, reuseMessageID string) string {
	return a.cards.sendPlanCardWithReuse(ctx, sub, planText, reuseMessageID)
}
func (a turnStreamOutboundCardAdapter) SendTurnItemCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, payload turnitem.CardPayload, reuseMessageID string) string {
	return a.cards.sendTurnItemCardWithReuse(ctx, sub, payload, reuseMessageID)
}
func (a turnStreamOutboundCardAdapter) CompleteStandaloneCompactItem(threadID, turnID string, item turnitem.ProtocolItem) bool {
	return a.compact.CompleteStandaloneCompactItem(threadID, turnID, item.MergedRaw())
}

type claudeTurnStreamPort struct {
	itemContext      appapproval.ItemContext
	turnPresentation *appturnstream.Service
}

func (p claudeTurnStreamPort) NoteTurnItemStarted(threadID, turnID string, item turnitem.ProtocolItem) {
	p.itemContext.Start(threadID, turnID, item)
}
func (p claudeTurnStreamPort) UpdateInFlightTurnItem(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
	p.turnPresentation.UpdateInFlightTurnItem(ctx, threadID, turnID, itemID, item)
}
func (p claudeTurnStreamPort) RecordTurnError(threadID, turnID, message string) {
	p.turnPresentation.RecordTurnError(threadID, turnID, message)
}
func (p claudeTurnStreamPort) CompleteTurnItem(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
	p.turnPresentation.CompleteTurnItem(ctx, threadID, turnID, itemID, item)
}
func (p claudeTurnStreamPort) PrepareTurnStreamQuietBoundary(turnID string) string {
	return p.turnPresentation.PrepareStreamQuietBoundary(turnID).ReuseMessageID
}
func (p claudeTurnStreamPort) PrepareTurnStreamQuietUpdate(sessionKey string, sub *domainsubmission.Submission, threadID, itemID string, item turnitem.ProtocolItem, workspaceCwd string) turn.QuietWorkingCardOp {
	return p.turnPresentation.PrepareStreamQuietUpdate(sessionKey, sub, threadID, itemID, item, workspaceCwd)
}
func (p claudeTurnStreamPort) MarkTurnStreamFinal(turnID string) {
	p.turnPresentation.MarkStreamFinal(turnID)
}

type TurnPresentationPortInputs struct {
	Runtime          BackendRuntimeDeps
	Turns            *appturn.Service
	TurnPresentation *appturnstream.Service
	Tracker          *appturnstream.Tracker
	Finder           appsubmission.SubmissionLookupService
	Items            *turnitem.Tracker
	Compaction       *compaction.Service
	SubmissionStatus appsubmission.StatusService
	Cards            OutboundCardService
}

func TurnPresentationPorts(inputs TurnPresentationPortInputs) appturnstream.Dependencies {
	runtimeDeps := inputs.Runtime
	owner := runtimeDeps.runtime.owner
	stateStore := runtimeDeps.stateView
	cards := inputs.Cards
	return appturnstream.Dependencies{
		Context: owner.Lifecycle.Context,
		Tracker: inputs.Tracker, Finder: inputs.Finder, Lifecycle: inputs.Turns, Runtime: turnItemsPort{tracker: inputs.Items},
		Outbound: turnStreamOutboundCardAdapter{cards: cards, compact: inputs.Compaction},
		Quiet: quietWorkingCardExecutor{
			renderer: cards.replyChunks.renderer, state: cards.replyChunks.state, outbound: cards.replyChunks.outbound,
			links: cards.links, turns: inputs.TurnPresentation, ready: cards.replyChunks.ready,
		},
		SendStartedNotice: func(ctx context.Context, sub *domainsubmission.Submission) {
			maybeSendSubmissionStartedNotice(inputs.SubmissionStatus, stateStore, cards, ctx, sub)
		},
		WorkspaceCwd: func(id string) string {
			if ws := config.FindWorkspace(runtimeDeps.cfg, id); ws != nil {
				return ws.Cwd
			}
			return ""
		},
		FeishuConfig: func() *config.FeishuConfig { return runtimeDeps.view.feishuConfig() },
	}
}

type turnItemsPort struct{ tracker *turnitem.Tracker }

func (p turnItemsPort) CompleteTurnItemState(threadID, turnID, itemID string, item turnitem.ProtocolItem) turnitem.ProtocolItem {
	return p.tracker.Complete(threadID, turnID, itemID, item)
}

func (p turnItemsPort) ClearTurnItemStates(turnID string) { p.tracker.ClearTurn(turnID) }
