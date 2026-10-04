package feishuapp

import (
	domainsubmission "feidex/internal/domain/submission"

	"context"
	"feidex/internal/domain/conversation"

	appturnlifecycle "feidex/internal/application/turn"
)

// ---------------------------------------------------------------------------
// Provider adapters — satisfy application turn use case ports
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// *App methods satisfying runtime and delivery ports
// ---------------------------------------------------------------------------

func (a *App) MarkSessionThreadLive(sessionKey, threadID string) {
	markSessionThreadLive(a, sessionKey, threadID)
}

func (a *App) TurnStopAttentionUserID(sub *domainsubmission.Submission, turnID string) string {
	return turnStopAttentionUserID(a.State(), sub, turnID)
}

func (a *App) SendEmptyFinalCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, footerLines []string, reuseMessageID string) string {
	return newOutboundCardService(a).replyChunks.SendEmptyFinalCardWithReuse(ctx, sub, footerLines, reuseMessageID)
}

func (a *App) SendFinalMessagesWithReuse(ctx context.Context, sub *domainsubmission.Submission, text string, footerLines []string, reuseMessageID string) []string {
	reuseIDs := []string(nil)
	if reuseMessageID != "" {
		reuseIDs = []string{reuseMessageID}
	}
	results := sendFinalMessagesWithFooterAndReuse(newOutboundCardService(a).replyChunks, ctx, sub, text, footerLines, replyInThreadForSubmission(sub), reuseIDs)
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.MessageID)
	}
	return ids
}

func (a *App) NextQueuedSubmissionSessionKey(sessionKey string) string {
	return a.bindings.Submissions.NextQueuedSessionKey(sessionKey)
}

func (a *App) BindStandaloneCompactTurn(threadID, turnID string) bool {
	return a.bindings.Compaction.BindStandaloneCompactTurn(threadID, turnID)
}

func (a *App) BindGoalContinuationTurn(threadID, turnID string) bool {
	return a.bindings.GoalContinuation.BindGoalContinuationTurn(threadID, turnID)
}

func (a *App) FinishStandaloneCompactTurn(threadID, turnID, status string) bool {
	return a.bindings.Compaction.FinishStandaloneCompactTurn(threadID, turnID, status)
}

func (a *App) FindSubmissionByTurn(threadID, turnID string) (string, *domainsubmission.Submission) {
	return findSubmissionByTurn(a.bindings.SubmissionLookup, threadID, turnID)
}

func (a *App) ProcessCodexPlanModeExitOnTurnCompleted(sessionKey string, sub *domainsubmission.Submission, threadID, turnID, status string, flush appturnlifecycle.TurnStreamFlushResult) bool {
	return processCodexPlanModeExitOnTurnCompleted(a, sessionKey, sub, threadID, turnID, status, flush)
}

func (a *App) LogSessionState(event, sessionKey string, sess *conversation.Session) {
	logSessionState(event, sessionKey, sess)
}
