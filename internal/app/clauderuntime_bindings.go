package app

import (
	"context"
	"time"

	appapproval "feidex/internal/app/approval"
	appclauderuntime "feidex/internal/app/clauderuntime"
	appdelivery "feidex/internal/app/delivery"
	apppendingforms "feidex/internal/app/pendingforms"
	appturn "feidex/internal/app/turn"
	"feidex/internal/app/turnitem"
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
	svc := appclauderuntime.NewService(appclauderuntime.Deps{
		App: app,
		Cfg: cfg,
		Lifecycle: appclauderuntime.LifecycleDeps{
			BindClaudeSessionThread: func(sessionKey, turnID, threadID string) {
				bindClaudeSessionThread(app, sessionKey, turnID, threadID)
			},
			FinishTurn: func(threadID, turnID, status string) {
				finishTurn(app, threadID, turnID, status)
			},
			FinishSteerSubmission: func(submissionID, status string) {
				finishSteerSubmission(app, submissionID, status)
			},
			FailClaudeSessionWork: func(sessionKey, threadID string, err error) {
				failClaudeSessionActiveWork(app, sessionKey, threadID, err)
			},
			FailBackendActiveWork: func(backend, sessionKey, threadID, message string) {
				failBackendActiveWork(app, backend, sessionKey, threadID, message)
			},
		},
		TurnStream: appclauderuntime.TurnStreamDeps{
			RecordTurnError: func(threadID, turnID, message string) {
				newTurnStreamService(app).recordTurnError(threadID, turnID, message)
			},
			CompleteTurnItem: func(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
				newTurnStreamService(app).completeTurnItemPayload(ctx, threadID, turnID, itemID, item)
			},
			PrepareTurnStreamQuietBoundary: func(turnID string) string {
				boundary := newTurnStreamService(app).prepareTurnStreamQuietBoundary(turnID)
				return boundary.ReuseMessageID
			},
			PrepareTurnStreamQuietUpdate: func(sessionKey string, sub *state.Submission, threadID, itemID string, item turnitem.ProtocolItem, workspaceCwd string) appturn.QuietWorkingCardOp {
				return newTurnStreamService(app).prepareTurnStreamQuietUpdatePayload(sessionKey, sub, threadID, itemID, item, workspaceCwd)
			},
			MarkTurnStreamFinal: func(turnID string) {
				newTurnStreamService(app).markTurnStreamFinal(turnID)
			},
		},
		Usage: appclauderuntime.UsageDeps{
			RecordClaudeThreadUsage: func(threadID string, usage claudecli.TurnUsage) {
				newUsageServiceInner(app).RecordClaudeThreadUsage(threadID, usage)
			},
			RecordTurnTokenUsage: func(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
				newRuntimeStateService(app).recordTurnTokenUsage(threadID, turnID, usage)
			},
			RecordTurnContextUsagePercent: func(turnID string, percent float64) {
				newRuntimeStateService(app).recordTurnContextUsagePercent(turnID, percent)
			},
			TurnFinalFooterLines: func(turnID string, completedAt time.Time) []string {
				return newRuntimeStateService(app).turnFinalFooterLines(turnID, completedAt)
			},
		},
		Delivery: appclauderuntime.DeliveryDeps{
			ExecuteQuietWorkingCardOp: func(ctx context.Context, sub *state.Submission, op appturn.QuietWorkingCardOp) {
				executeQuietWorkingCardOp(app, ctx, sub, op)
			},
			UpdateOutputSegment: func(ctx context.Context, threadID, turnID, body, reuseMessageID string) ([]appdelivery.SentReplyChunk, bool) {
				return updateClaudeOutputSegmentWithReuse(app, ctx, threadID, turnID, body, reuseMessageID)
			},
			FinalizeOutputSegment: func(ctx context.Context, threadID, turnID, body string) bool {
				return finalizeClaudeOutputSegment(app, ctx, threadID, turnID, body)
			},
			SendFinalMessages: func(ctx context.Context, sub *state.Submission, text string, footerLines []string, inThread bool, reuseMessageIDs []string) []appdelivery.SentReplyChunk {
				return sendFinalMessagesWithFooterAndReuse(app, ctx, sub, text, footerLines, inThread, reuseMessageIDs)
			},
			ReplyInThread: func(sub *state.Submission) bool {
				return replyInThreadForSubmission(app, sub)
			},
		},
		Interactive: appclauderuntime.InteractiveDeps{
			SendClaudeApprovalCard: func(requestID, sessionKey string, sub *state.Submission, presentation appapproval.Presentation) error {
				return sendClaudeApprovalCard(app, requestID, sessionKey, sub, presentation)
			},
			SendClaudeUserInputCard: func(requestID, sessionKey string, sub *state.Submission, payload apppendingforms.ToolUserInputPayload) error {
				return sendClaudeUserInputCard(app, requestID, sessionKey, sub, payload)
			},
			SendClaudeUserInputFormCard: func(requestID, sessionKey string, sub *state.Submission, payload apppendingforms.ToolUserInputPayload) error {
				return sendClaudeUserInputFormCard(app, requestID, sessionKey, sub, payload)
			},
			SendClaudePlanModeCard: func(requestID, sessionKey string, sub *state.Submission, threadID, turnID, body string) error {
				return sendClaudePlanModeCard(app, requestID, sessionKey, sub, threadID, turnID, body)
			},
		},
		Lookup: appclauderuntime.LookupDeps{
			FindSubmissionByTurn: func(threadID, turnID string) (string, *state.Submission) {
				return newSubmissionQueueServiceFromApp(app).FindSubmissionByTurn(threadID, turnID)
			},
			GetSession: func(sessionKey string) *state.Session {
				return app.State().Session(sessionKey)
			},
			SessionHasActiveOps: func(sess *state.Session) bool {
				return sessionHasActiveOperations(sess)
			},
			NextLocalID: func(prefix string) (string, error) {
				return app.State().NextLocalID(prefix)
			},
			WorkspaceCwd: func(workspaceID string) string {
				return workspaceCwd(app.cfg, workspaceID)
			},
		},
		Permission: appclauderuntime.PermissionDeps{
			EffectivePermissionMode: func(sess *state.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
				return effectiveClaudePermissionMode(sess, ws, cfg)
			},
			QuietWorkingCardEnabled: func() bool {
				return quietWorkingCardEnabled(feishuConfig(app))
			},
		},
	})

	return &claudeRuntime{app: app, service: svc}
}
