package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/planmode"
	appturnstream "feidex/internal/adapter/feishu/turnstream"
	"feidex/internal/application/announcement"
	"feidex/internal/application/compaction"
	"feidex/internal/application/goal"
	applicationturn "feidex/internal/application/turn"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	domainsubmission "feidex/internal/domain/submission"
	frontendruntime "feidex/internal/runtime"
	"strings"
)

// These small ports keep the turn use case independent from the transitional
// App aggregate. They are composed here, where runtime, continuation and
// delivery owners are known.
type turnRuntimePort struct {
	lifecycle   *frontendruntime.FrontendRuntime
	asyncRunner func(func())
	liveThreads liveThreadMarker
}

func (p turnRuntimePort) Context() context.Context {
	if p.lifecycle == nil {
		return context.Background()
	}
	return p.lifecycle.Context()
}
func (p turnRuntimePort) RunAsync(fn func()) {
	if p.lifecycle != nil {
		p.lifecycle.Run(fn, p.asyncRunner)
	}
}
func (p turnRuntimePort) MarkSessionThreadLive(sessionKey, threadID string) {
	p.liveThreads.MarkSessionThreadLive(sessionKey, threadID)
}

type liveThreadMarker struct {
	tracker      *frontendruntime.LiveThreads
	state        planmode.SessionStateProvider
	announcement announcement.Query
	refreshes    *frontendruntime.CoalescedRefresh
}

func (p liveThreadMarker) MarkSessionThreadLive(sessionKey, threadID string) {
	if strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(threadID) == "" {
		return
	}
	if p.tracker != nil {
		p.tracker.Mark(sessionKey, threadID)
	}
	if p.state == nil {
		return
	}
	sess := p.state.Session(sessionKey)
	if sess == nil {
		return
	}
	chatID := strings.TrimSpace(sess.ChatID)
	if chatID == "" {
		_, _, chatID, _, _ = identity.ParseSessionKey(sess.Key)
	}
	if p.announcement.GroupSession(sess, chatID) && p.refreshes != nil {
		p.refreshes.Schedule(chatID)
	}
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

type turnDeliveryPort struct {
	state       turnStopStateProvider
	replyChunks replyChunkDelivery
}

func (p turnDeliveryPort) TurnStopAttentionUserID(sub *domainsubmission.Submission, turnID string) string {
	return turnStopAttentionUserID(p.state, sub, turnID)
}
func (p turnDeliveryPort) SendEmptyFinalCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, footerLines []string, reuseMessageID string) string {
	return p.replyChunks.SendEmptyFinalCardWithReuse(ctx, sub, footerLines, reuseMessageID)
}
func (p turnDeliveryPort) SendFinalMessagesWithReuse(ctx context.Context, sub *domainsubmission.Submission, text string, footerLines []string, reuseMessageID string) []string {
	return p.replyChunks.SendFinalMessagesWithFooter(ctx, sub, text, footerLines, replyInThreadForSubmission(sub), reuseMessageID)
}

type turnDiagnosticsPort struct{}

func (p turnDiagnosticsPort) LogSessionState(event, sessionKey string, sess *conversation.Session) {
	logSessionState(event, sessionKey, sess)
}

func TurnPorts(app *App, turnPresentation *appturnstream.Service) applicationturn.Dependencies {
	owner := app.runtimeOwner
	outboundCards := newOutboundCardService(app)
	return applicationturn.Dependencies{
		State: app.State(), Bindings: app.bindings.TurnMetadata,
		Replies: app.bindings.Continuation, Streams: turnPresentation,
		Reactions: app.bindings.PendingQueue, Cards: outboundCards,
		Queue: app.bindings.Submissions, Retry: app.bindings.AutoRetry,
		Cleanup: app.bindings.SubmissionCleanup,
		Runtime: turnRuntimePort{lifecycle: &owner.Lifecycle, asyncRunner: app.asyncRunner, liveThreads: liveThreadMarker{
			tracker: owner.LiveThreads, state: app.State(), announcement: app.bindings.AnnouncementQuery, refreshes: owner.Announcements,
		}}, RunSessionAsync: func(sessionKey string, fn func()) {
			if fn == nil {
				return
			}
			runAsync(app, func() { app.sessionActorRuntime().Run("session:"+strings.TrimSpace(sessionKey), fn) })
		}, Continuations: turnContinuationPort{compaction: app.bindings.Compaction, goal: app.bindings.GoalContinuation, plan: newPlanModeAppAdapter(app)},
		Delivery: turnDeliveryPort{state: app.State(), replyChunks: outboundCards.replyChunks}, Diagnostics: turnDiagnosticsPort{},
	}
}
