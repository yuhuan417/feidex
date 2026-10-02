package app

import (
	"context"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"log/slog"
	"time"

	appapproval "feidex/internal/adapter/feishu/approval"

	appclauderuntime "feidex/internal/runtime/claude"

	appdelivery "feidex/internal/adapter/feishu/delivery"

	apppendingforms "feidex/internal/adapter/feishu/pendingforms"
	"feidex/internal/adapter/feishu/quietmode"

	appturn "feidex/internal/adapter/feishu/turn"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/config"

	domainmodelconfig "feidex/internal/domain/modelconfig"
)

// claudeRuntime wraps *clauderuntime.Service with *App-specific callbacks.
type claudeRuntime struct {
	app     *App
	service *appclauderuntime.Service
}

func newClaudeRuntime(app *App, cfg config.ClaudeConfig) ClaudeCore {
	svc := appclauderuntime.NewService(appclauderuntime.Deps{
		Context: app.Context,
		Cfg:     cfg,
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
			Port: claudeTurnStreamPort{app: app},
			NoteTurnItemStarted: func(threadID, turnID string, item turnitem.ProtocolItem) {
				newRuntimeStateService(app).noteTurnItemStartedPayload(threadID, turnID, item)
			},
			UpdateInFlightTurnItem: func(ctx context.Context, threadID, turnID, itemID string, item turnitem.ProtocolItem) {
				newTurnStreamService(app).updateInFlightTurnItemPayload(ctx, threadID, turnID, itemID, item)
			},
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
			PrepareTurnStreamQuietUpdate: func(sessionKey string, sub *domainsubmission.Submission, threadID, itemID string, item turnitem.ProtocolItem, workspaceCwd string) appturn.QuietWorkingCardOp {
				return newTurnStreamService(app).prepareTurnStreamQuietUpdatePayload(sessionKey, sub, threadID, itemID, item, workspaceCwd)
			},
			MarkTurnStreamFinal: func(turnID string) {
				newTurnStreamService(app).markTurnStreamFinal(turnID)
			},
		},
		Usage: appclauderuntime.UsageDeps{
			RecordClaudeThreadUsage: func(threadID string, usage claudecli.TurnUsage) {
				newUsageService(app).RecordClaudeThreadUsage(threadID, usage)
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
			ExecuteQuietWorkingCardOp: func(ctx context.Context, sub *domainsubmission.Submission, op appturn.QuietWorkingCardOp) {
				executeQuietWorkingCardOp(app, ctx, sub, op)
			},
			UpdateOutputSegment: func(ctx context.Context, threadID, turnID, body, reuseMessageID string) ([]appdelivery.SentReplyChunk, bool) {
				return updateClaudeOutputSegmentWithReuse(app, ctx, threadID, turnID, body, reuseMessageID)
			},
			FinalizeOutputSegment: func(ctx context.Context, threadID, turnID, body string) bool {
				return finalizeClaudeOutputSegment(app, ctx, threadID, turnID, body)
			},
			SendFinalMessages: func(ctx context.Context, sub *domainsubmission.Submission, text string, footerLines []string, inThread bool, reuseMessageIDs []string) []appdelivery.SentReplyChunk {
				return sendFinalMessagesWithFooterAndReuse(app, ctx, sub, text, footerLines, inThread, reuseMessageIDs)
			},
			ReplyInThread: func(sub *domainsubmission.Submission) bool {
				return replyInThreadForSubmission(app, sub)
			},
			SendBackgroundTaskNotification: func(ctx context.Context, target appclauderuntime.BackgroundTaskTarget, event claudecli.BackgroundTaskEvent) {
				sendClaudeBackgroundTaskNotification(app, ctx, target, event)
			},
		},
		Interactive: appclauderuntime.InteractiveDeps{
			SendClaudeApprovalCard: func(requestID, sessionKey string, sub *domainsubmission.Submission, presentation appapproval.Presentation) error {
				return sendClaudeApprovalCard(app, requestID, sessionKey, sub, presentation)
			},
			SendClaudeUserInputCard: func(requestID, sessionKey string, sub *domainsubmission.Submission, payload apppendingforms.ToolUserInputPayload) error {
				return sendClaudeUserInputCard(app, requestID, sessionKey, sub, payload)
			},
			SendClaudeUserInputFormCard: func(requestID, sessionKey string, sub *domainsubmission.Submission, payload apppendingforms.ToolUserInputPayload) error {
				return sendClaudeUserInputFormCard(app, requestID, sessionKey, sub, payload)
			},
			SendClaudePlanModeCard: func(requestID, sessionKey string, sub *domainsubmission.Submission, threadID, turnID, body string) error {
				return sendClaudePlanModeCard(app, requestID, sessionKey, sub, threadID, turnID, body)
			},
			SendDetachedApprovalCard: func(requestID string, target appclauderuntime.InteractionTarget, presentation appapproval.Presentation) error {
				return newClaudeSupportService(app).SendDetachedApprovalCard(requestID, target, presentation)
			},
			SendDetachedUserInputCard: func(requestID string, target appclauderuntime.InteractionTarget, payload apppendingforms.ToolUserInputPayload) error {
				return newClaudeSupportService(app).SendDetachedUserInputCard(requestID, target, payload)
			},
			SendDetachedUserInputFormCard: func(requestID string, target appclauderuntime.InteractionTarget, payload apppendingforms.ToolUserInputPayload) error {
				return newClaudeSupportService(app).SendDetachedUserInputFormCard(requestID, target, payload)
			},
			SendDetachedPlanModeCard: func(requestID string, target appclauderuntime.InteractionTarget, body string) error {
				return newClaudeSupportService(app).SendDetachedPlanModeCard(requestID, target, body)
			},
			ExpireInteractionCards: func(sessionKey string, requestIDs []string, reason string) {
				ExpireClaudeInteractionCards(app, sessionKey, requestIDs, reason)
			},
		},
		Lookup: appclauderuntime.LookupDeps{
			FindSubmissionByTurn: func(threadID, turnID string) (string, *domainsubmission.Submission) {
				return newSubmissionQueueServiceFromApp(app).FindSubmissionByTurn(threadID, turnID)
			},
			GetSession: func(sessionKey string) *conversation.Session {
				return app.State().Session(sessionKey)
			},
			SessionHasActiveOps: func(sess *conversation.Session) bool {
				return conversation.HasActiveOperations(sess)
			},
			NextLocalID: func(prefix string) (string, error) {
				return app.State().NextLocalID(prefix)
			},
			WorkspaceCwd: func(workspaceID string) string {
				return workspaceCwd(app.cfg, workspaceID)
			},
		},
		Permission: appclauderuntime.PermissionDeps{
			EffectivePermissionMode: func(sess *conversation.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
				return effectiveBindingClaudePermissionMode(app, sess, ws, cfg)
			},
			QuietWorkingCardEnabled: func() bool {
				return quietmode.WorkingCardEnabled(feishuConfig(app))
			},
		},
		PrepareClaudeMCPConfig: func(sessionKey string) (string, []string, func(), error) {
			return prepareClaudeMCPConfig(app, sessionKey)
		},
		ModelSettings: func(sessionKey string) domainmodelconfig.Snapshot {
			sess := app.State().Session(normalizeSessionKey(app, sessionKey))
			return modelConfigSnapshot(app, sess, backendClaude)
		},
		ModelSettingsApplied: func(sessionKey string, settings domainmodelconfig.Snapshot) {
			_, err := app.State().UpdateSession(sessionKey, func(sess *conversation.Session) {
				sess.AppliedModelConfig = settings
				sess.ModelConfigError = ""
			})
			if err != nil {
				slog.Warn("record Claude model settings failed", "session_key", sessionKey, "error", err)
			}
		},
		AuxiliaryModels: func(sessionKey string) (string, string) {
			sess := app.State().Session(normalizeSessionKey(app, sessionKey))
			return effectiveClaudeSmallModel(app, sess), effectiveClaudeSubagentModel(app, sess)
		},
	})

	return &claudeRuntime{app: app, service: svc}
}
