package feishuapp

import (
	"context"
	retryview "feidex/internal/adapter/feishu/autoretry"
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/adapter/feishu/turnmeta"
	appturnstream "feidex/internal/adapter/feishu/turnstream"
	"feidex/internal/application/announcement"
	"feidex/internal/application/compaction"
	"feidex/internal/application/continuation"
	"feidex/internal/application/goal"
	"feidex/internal/application/submission"
	applicationturn "feidex/internal/application/turn"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	domainsubmission "feidex/internal/domain/submission"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/runtime/maintenance"
	"strings"
)

// These small ports keep the turn use case independent from the transitional
// frontend shell. They are composed here, where runtime, continuation and
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
	plan       *planmode.Dependencies
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
	if p.plan == nil {
		return false
	}
	return planmode.ProcessCodexPlanModeExitOnTurnCompleted(*p.plan, sessionKey, sub, threadID, turnID, status, planmode.TurnStreamFlushResult{
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

type TurnPortInputs struct {
	Runtime           BackendRuntimeDeps
	TurnPresentation  *appturnstream.Service
	Cards             OutboundCardService
	TurnMetadata      turnmeta.Service
	Continuation      *continuation.Service
	PendingQueue      *submission.PendingQueueService
	Submissions       *submission.SubmissionQueueService
	AutoRetry         retryview.Service
	SubmissionCleanup maintenance.SubmissionCleanup
	Compaction        *compaction.Service
	GoalContinuation  *goal.Service
	PlanMode          *planmode.Dependencies
	AnnouncementQuery announcement.Query
	AsyncRunner       func(func())
}

func TurnPorts(inputs TurnPortInputs) applicationturn.Dependencies {
	runtimeDeps := inputs.Runtime
	owner := runtimeDeps.runtime.owner
	if owner == nil {
		return applicationturn.Dependencies{}
	}
	outboundCards := inputs.Cards
	state := runtimeDeps.stateView
	return applicationturn.Dependencies{
		State: state, Bindings: inputs.TurnMetadata,
		Replies: inputs.Continuation, Streams: inputs.TurnPresentation,
		Reactions: inputs.PendingQueue, Cards: outboundCards,
		Queue: inputs.Submissions, Retry: inputs.AutoRetry,
		Cleanup: inputs.SubmissionCleanup,
		Runtime: turnRuntimePort{lifecycle: &owner.Lifecycle, asyncRunner: inputs.AsyncRunner, liveThreads: liveThreadMarker{
			tracker: owner.LiveThreads, state: state, announcement: inputs.AnnouncementQuery, refreshes: owner.Announcements,
		}}, RunSessionAsync: func(sessionKey string, fn func()) {
			if fn == nil {
				return
			}
			owner.Lifecycle.Run(func() { runSessionOnActor(runtimeDeps.sessionActors, sessionKey, fn) }, inputs.AsyncRunner)
		}, Continuations: turnContinuationPort{compaction: inputs.Compaction, goal: inputs.GoalContinuation, plan: inputs.PlanMode},
		Delivery: turnDeliveryPort{state: state, replyChunks: outboundCards.replyChunks}, Diagnostics: turnDiagnosticsPort{},
	}
}
