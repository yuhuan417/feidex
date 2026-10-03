package feishuapp

import (
	"context"
	applicationturn "feidex/internal/application/turn"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"strings"
)

// These small ports keep the turn use case independent from the transitional
// App aggregate. They are composed here, where runtime, continuation and
// delivery owners are known.
type turnRuntimePort struct{ app *App }

func (p turnRuntimePort) Context() context.Context { return p.app.Context() }
func (p turnRuntimePort) RunAsync(fn func())       { runAsync(p.app, fn) }
func (p turnRuntimePort) MarkSessionThreadLive(sessionKey, threadID string) {
	markSessionThreadLive(p.app, sessionKey, threadID)
}

type turnContinuationPort struct{ app *App }

func (p turnContinuationPort) BindStandaloneCompactTurn(threadID, turnID string) bool {
	return newCompactionService(p.app).BindStandaloneCompactTurn(threadID, turnID)
}
func (p turnContinuationPort) BindGoalContinuationTurn(threadID, turnID string) bool {
	return newGoalService(p.app).BindGoalContinuationTurn(threadID, turnID)
}
func (p turnContinuationPort) FinishStandaloneCompactTurn(threadID, turnID, status string) bool {
	return newCompactionService(p.app).FinishStandaloneCompactTurn(threadID, turnID, status)
}
func (p turnContinuationPort) ProcessCodexPlanModeExitOnTurnCompleted(sessionKey string, sub *domainsubmission.Submission, threadID, turnID, status string, flush applicationturn.TurnStreamFlushResult) bool {
	return processCodexPlanModeExitOnTurnCompleted(p.app, sessionKey, sub, threadID, turnID, status, flush)
}

type turnDeliveryPort struct{ app *App }

func (p turnDeliveryPort) TurnStopAttentionUserID(sub *domainsubmission.Submission, turnID string) string {
	return turnStopAttentionUserID(p.app, sub, turnID)
}
func (p turnDeliveryPort) SendEmptyFinalCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, footerLines []string, reuseMessageID string) string {
	return sendEmptyFinalCardWithReuse(p.app, ctx, sub, footerLines, reuseMessageID)
}
func (p turnDeliveryPort) SendFinalMessagesWithReuse(ctx context.Context, sub *domainsubmission.Submission, text string, footerLines []string, reuseMessageID string) []string {
	return p.app.SendFinalMessagesWithReuse(ctx, sub, text, footerLines, reuseMessageID)
}

type turnDiagnosticsPort struct{ app *App }

func (p turnDiagnosticsPort) LogSessionState(event, sessionKey string, sess *conversation.Session) {
	logSessionState(event, sessionKey, sess)
}

func newTurnLifecycleService(app *App) applicationturn.Service {
	return applicationturn.NewService(applicationturn.Dependencies{
		State: app.State(), Bindings: newRuntimeStateService(app),
		Replies: newReplyContinuationService(app), Streams: newTurnStreamService(app),
		Reactions: newPendingQueueService(app), Cards: newOutboundCardService(app),
		Queue: newSubmissionQueueServiceFromApp(app), Retry: newAutoRetryService(app),
		Cleanup: newSubmissionCleanup(app),
		Runtime: turnRuntimePort{app: app}, RunSessionAsync: func(sessionKey string, fn func()) {
			if fn == nil {
				return
			}
			runAsync(app, func() { app.sessionActorRuntime().Run("session:"+strings.TrimSpace(sessionKey), fn) })
		}, Continuations: turnContinuationPort{app: app},
		Delivery: turnDeliveryPort{app: app}, Diagnostics: turnDiagnosticsPort{app: app},
	})
}
