package app

import (
	"context"
	"time"

	appclauderuntime "feidex/internal/app/clauderuntime"
	appdelivery "feidex/internal/app/delivery"
	apppendingforms "feidex/internal/app/pendingforms"
	appturn "feidex/internal/app/turn"
	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

// claudeRuntime wraps *clauderuntime.Service with *App-specific callbacks.
type claudeRuntime struct {
	app     *App
	service *appclauderuntime.Service
}

func newClaudeRuntime(app *App, cfg config.ClaudeConfig) ClaudeCore {
	svc := appclauderuntime.NewService(app, cfg)

	// Lifecycle callbacks
	svc.BindClaudeSessionThread = func(sessionKey, turnID, threadID string) {
		bindClaudeSessionThread(app, sessionKey, turnID, threadID)
	}
	svc.FinishTurn = func(threadID, turnID, status string) {
		finishTurn(app, threadID, turnID, status)
	}
	svc.FinishSteerSubmission = func(submissionID, status string) {
		finishSteerSubmission(app, submissionID, status)
	}
	svc.FailClaudeSessionWork = func(sessionKey, threadID string, err error) {
		failClaudeSessionActiveWork(app, sessionKey, threadID, err)
	}
	svc.FailBackendActiveWork = func(backend, sessionKey, threadID, message string) {
		failBackendActiveWork(app, backend, sessionKey, threadID, message)
	}

	// Turn-stream callbacks
	svc.RecordTurnError = func(threadID, turnID, message string) {
		newTurnStreamService(app).recordTurnError(threadID, turnID, message)
	}
	svc.CompleteTurnItem = func(ctx context.Context, threadID, turnID, itemID string, item map[string]any) {
		newTurnStreamService(app).completeTurnItem(ctx, threadID, turnID, itemID, item)
	}
	svc.PrepareTurnStreamQuietBoundary = func(turnID string) string {
		boundary := newTurnStreamService(app).prepareTurnStreamQuietBoundary(turnID)
		return boundary.ReuseMessageID
	}
	svc.PrepareTurnStreamQuietUpdate = func(sessionKey string, sub *state.Submission, threadID, itemID string, item map[string]any, workspaceCwd string) appturn.QuietWorkingCardOp {
		return newTurnStreamService(app).prepareTurnStreamQuietUpdate(sessionKey, sub, threadID, itemID, item, workspaceCwd)
	}
	svc.MarkTurnStreamFinal = func(turnID string) {
		newTurnStreamService(app).markTurnStreamFinal(turnID)
	}

	// Usage callbacks
	svc.RecordClaudeThreadUsage = func(threadID string, usage claudecli.TurnUsage) {
		newUsageService(app).RecordClaudeThreadUsage(threadID, usage)
	}
	svc.RecordTurnTokenUsage = func(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
		newRuntimeStateService(app).recordTurnTokenUsage(threadID, turnID, usage)
	}
	svc.RecordTurnContextUsagePercent = func(turnID string, percent float64) {
		newRuntimeStateService(app).recordTurnContextUsagePercent(turnID, percent)
	}
	svc.TurnFinalFooterLines = func(turnID string, completedAt time.Time) []string {
		return newRuntimeStateService(app).turnFinalFooterLines(turnID, completedAt)
	}

	// Delivery callbacks
	svc.ExecuteQuietWorkingCardOp = func(ctx context.Context, sub *state.Submission, op appturn.QuietWorkingCardOp) {
		executeQuietWorkingCardOp(app, ctx, sub, op)
	}
	svc.UpdateOutputSegment = func(ctx context.Context, threadID, turnID, body, reuseMessageID string) ([]appdelivery.SentReplyChunk, bool) {
		return updateClaudeOutputSegmentWithReuse(app, ctx, threadID, turnID, body, reuseMessageID)
	}
	svc.FinalizeOutputSegment = func(ctx context.Context, threadID, turnID, body string) bool {
		return finalizeClaudeOutputSegment(app, ctx, threadID, turnID, body)
	}
	svc.SendFinalMessages = func(ctx context.Context, sub *state.Submission, text string, footerLines []string, inThread bool, reuseMessageIDs []string) []appdelivery.SentReplyChunk {
		return sendFinalMessagesWithFooterAndReuse(app, ctx, sub, text, footerLines, inThread, reuseMessageIDs)
	}
	svc.ReplyInThread = func(sub *state.Submission) bool {
		return replyInThreadForSubmission(app, sub)
	}

	// Card-sending callbacks
	svc.SendClaudeApprovalCard = func(kind, requestID, sessionKey string, sub *state.Submission, threadID, turnID, itemID, body string, requestPayload map[string]any, sessionActionLabel string) error {
		return sendClaudeApprovalCardWithPayload(app, kind, requestID, sessionKey, sub, threadID, turnID, itemID, body, requestPayload, sessionActionLabel)
	}
	svc.SendClaudeUserInputCard = func(requestID, sessionKey string, sub *state.Submission, payload apppendingforms.ToolUserInputPayload) error {
		return sendClaudeUserInputCard(app, requestID, sessionKey, sub, payload)
	}
	svc.SendClaudeUserInputFormCard = func(requestID, sessionKey string, sub *state.Submission, payload apppendingforms.ToolUserInputPayload) error {
		return sendClaudeUserInputFormCard(app, requestID, sessionKey, sub, payload)
	}
	svc.SendClaudePlanModeCard = func(requestID, sessionKey string, sub *state.Submission, threadID, turnID, body string) error {
		return sendClaudePlanModeCard(app, requestID, sessionKey, sub, threadID, turnID, body)
	}

	// Submission/session lookup callbacks
	svc.FindSubmissionByTurn = func(threadID, turnID string) (string, *state.Submission) {
		return findSubmissionByTurn(app, threadID, turnID)
	}
	svc.GetSession = func(sessionKey string) *state.Session {
		return app.State().Session(sessionKey)
	}
	svc.SessionHasActiveOps = func(sess *state.Session) bool {
		return sessionHasActiveOperations(sess)
	}
	svc.NextLocalID = func(prefix string) (string, error) {
		return app.State().NextLocalID(prefix)
	}
	svc.WorkspaceCwd = func(workspaceID string) string {
		return workspaceCwd(app.cfg, workspaceID)
	}

	// Permission mode callbacks
	svc.EffectivePermissionMode = func(sess *state.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
		return effectiveClaudePermissionMode(sess, ws, cfg)
	}
	svc.QuietWorkingCardEnabled = func() bool {
		return quietWorkingCardEnabled(feishuConfig(app))
	}

	// CleanupStaleSessionOps is handled by the service's own cleanupStaleSessionOps
	// method, which uses the GetSession, SessionHasActiveOps, FinishTurn, and
	// FailBackendActiveWork callbacks above. No separate callback needed.

	return &claudeRuntime{app: app, service: svc}
}
