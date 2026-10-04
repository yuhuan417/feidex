package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/planmode"
	appturnstream "feidex/internal/adapter/feishu/turnstream"
	"feidex/internal/application/compaction"
	"feidex/internal/application/goal"
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

type turnContinuationPort struct {
	compaction *compaction.Service
	goal       *goal.Service
	plan       planmode.Dependencies
}

func (p turnContinuationPort) BindStandaloneCompactTurn(threadID, turnID string) bool {
	return p.compaction.BindStandaloneCompactTurn(threadID, turnID)
}
func (p turnContinuationPort) BindGoalContinuationTurn(threadID, turnID string) bool {
	return p.goal.BindGoalContinuationTurn(threadID, turnID)
}
func (p turnContinuationPort) FinishStandaloneCompactTurn(threadID, turnID, status string) bool {
	return p.compaction.FinishStandaloneCompactTurn(threadID, turnID, status)
}
func (p turnContinuationPort) ProcessCodexPlanModeExitOnTurnCompleted(sessionKey string, sub *domainsubmission.Submission, threadID, turnID, status string, flush applicationturn.TurnStreamFlushResult) bool {
	return planmode.ProcessCodexPlanModeExitOnTurnCompleted(p.plan, sessionKey, sub, threadID, turnID, status, planmode.TurnStreamFlushResult{
		ShouldUsePlanExitPrompt: flush.ShouldUsePlanExitPrompt, PlanMarkdown: flush.PlanMarkdown,
		PlanMessageID: flush.PlanMessageID,
	})
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

type turnDiagnosticsPort struct{}

func (p turnDiagnosticsPort) LogSessionState(event, sessionKey string, sess *conversation.Session) {
	logSessionState(event, sessionKey, sess)
}

func TurnPorts(app *App, turnPresentation *appturnstream.Service) applicationturn.Dependencies {
	return applicationturn.Dependencies{
		State: app.State(), Bindings: app.bindings.TurnMetadata,
		Replies: app.bindings.Continuation, Streams: turnPresentation,
		Reactions: app.bindings.PendingQueue, Cards: newOutboundCardService(app),
		Queue: app.bindings.Submissions, Retry: app.bindings.AutoRetry,
		Cleanup: app.bindings.SubmissionCleanup,
		Runtime: turnRuntimePort{app: app}, RunSessionAsync: func(sessionKey string, fn func()) {
			if fn == nil {
				return
			}
			runAsync(app, func() { app.sessionActorRuntime().Run("session:"+strings.TrimSpace(sessionKey), fn) })
		}, Continuations: turnContinuationPort{compaction: app.bindings.Compaction, goal: app.bindings.GoalContinuation, plan: newPlanModeAppAdapter(app)},
		Delivery: turnDeliveryPort{app: app}, Diagnostics: turnDiagnosticsPort{},
	}
}
