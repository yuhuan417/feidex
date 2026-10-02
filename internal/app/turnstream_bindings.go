package app

import (
	"context"
	"feidex/internal/adapter/feishu/turn"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/config"
	domainsubmission "feidex/internal/domain/submission"

	appturnstream "feidex/internal/adapter/feishu/turnstream"
)

// ---------------------------------------------------------------------------
// Provider adapters — satisfy turnstream narrow interfaces
// ---------------------------------------------------------------------------

type turnStreamSubmissionFinderAdapter struct{ app *App }

func (a turnStreamSubmissionFinderAdapter) FindSubmissionByTurn(threadID, turnID string) (string, *domainsubmission.Submission) {
	return newSubmissionQueueServiceFromApp(a.app).FindSubmissionByTurn(threadID, turnID)
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

func newTurnPresentation(a *App) appturnstream.Service {
	if a.trackers.turnStreams == nil {
		a.trackers.turnStreams = appturnstream.NewTracker()
	}
	return appturnstream.NewService(appturnstream.Dependencies{
		Tracker: a.trackers.turnStreams, Finder: turnStreamSubmissionFinderAdapter{app: a}, Lifecycle: turnStreamTurnLifecycleAdapter{app: a}, Runtime: newRuntimeStateService(a),
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
