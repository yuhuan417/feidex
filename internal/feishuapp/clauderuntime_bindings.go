package feishuapp

import (
	"context"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	domainturn "feidex/internal/domain/turn"
	"log/slog"
	"time"

	appapproval "feidex/internal/adapter/feishu/approval"
	"feidex/internal/application"

	appclauderuntime "feidex/internal/runtime/claude"

	appdelivery "feidex/internal/adapter/feishu/delivery"

	apppendingforms "feidex/internal/adapter/feishu/pendingforms"
	"feidex/internal/adapter/feishu/quietmode"

	appturn "feidex/internal/adapter/feishu/turn"
	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/config"

	codexadapter "feidex/internal/adapter/backend/codex"
	domainmodelconfig "feidex/internal/domain/modelconfig"
)

func ClaudeRuntimePorts(app *App, cfg config.ClaudeConfig) appclauderuntime.Deps {
	return appclauderuntime.Deps{
		Context: app.Context,
		Cfg:     cfg,
		Lifecycle: appclauderuntime.LifecycleDeps{
			BindClaudeSessionThread: func(sessionKey, turnID, threadID string) {
				runSession(app, sessionKey, func() {
					bindClaudeSessionThread(app, sessionKey, turnID, threadID)
				})
			},
			FinishTurn: func(threadID, turnID, status string) {
				runSession(app, sessionKeyForBackendEvent(app.BackendRuntimeDeps(), application.BackendEvent{ThreadID: threadID}), func() {
					finishTurn(app.bindings.Turns, threadID, turnID, status)
				})
			},
			FinishSteerSubmission: func(submissionID, status string) {
				sessionKey := ""
				if sub := app.State().Submission(submissionID); sub != nil {
					sessionKey = sub.SessionKey
				}
				runSession(app, sessionKey, func() {
					app.bindings.Turns.FinishSteerSubmission(submissionID, status)
				})
			},
			FailClaudeSessionWork: func(sessionKey, threadID string, err error) {
				runSession(app, sessionKey, func() {
					failClaudeSessionActiveWork(app.bindings.BackendFailure, sessionKey, threadID, err)
				})
			},
			FailBackendActiveWork: func(backend, sessionKey, threadID, message string) {
				failBackendActiveWork(app, backend, sessionKey, threadID, message)
			},
		},
		TurnStream: appclauderuntime.TurnStreamDeps{
			Port: claudeTurnStreamPort{app: app},
		},
		Usage: appclauderuntime.UsageDeps{
			RecordClaudeThreadUsage: func(threadID string, usage claudecli.TurnUsage) {
				app.bindings.Usage.RecordClaudeThreadUsage(threadID, domainturn.ClaudeThreadUsage{
					InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
					CacheReadTokens: usage.CacheReadTokens, CacheCreationTokens: usage.CacheCreationTokens,
					CumulativeInputTokens: usage.CumulativeInputTokens, CumulativeOutputTokens: usage.CumulativeOutputTokens,
					CumulativeCacheReadTokens: usage.CumulativeCacheReadTokens, CumulativeCacheCreationTokens: usage.CumulativeCacheCreationTokens,
					HasCumulativeUsage: usage.HasCumulativeUsage, ContextWindow: usage.ContextWindow, CostUSD: usage.CostUSD,
				})
			},
			RecordTurnTokenUsage: func(threadID, turnID string, usage codexrpc.ThreadTokenUsage) {
				app.runtimeOwner.TurnBindings.RecordTurnTokenUsage(threadID, turnID, codexadapter.ThreadUsage(usage))
			},
			RecordTurnContextUsagePercent: func(turnID string, percent float64) {
				app.runtimeOwner.TurnBindings.RecordTurnContextUsagePercent(turnID, percent)
			},
			TurnFinalFooterLines: func(turnID string, completedAt time.Time) []string {
				return app.bindings.TurnMetadata.TurnFinalFooterLines(turnID, completedAt)
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
				return sendClaudeUserInputCard(app.bindings.ClaudeSupport, requestID, sessionKey, sub, payload)
			},
			SendClaudeUserInputFormCard: func(requestID, sessionKey string, sub *domainsubmission.Submission, payload apppendingforms.ToolUserInputPayload) error {
				return sendClaudeUserInputFormCard(app.bindings.ClaudeSupport, requestID, sessionKey, sub, payload)
			},
			SendClaudePlanModeCard: func(requestID, sessionKey string, sub *domainsubmission.Submission, threadID, turnID, body string) error {
				return sendClaudePlanModeCard(app.bindings.ClaudeSupport, requestID, sessionKey, sub, threadID, turnID, body)
			},
			SendDetachedApprovalCard: func(requestID string, target appclauderuntime.InteractionTarget, presentation appapproval.Presentation) error {
				return app.bindings.ClaudeSupport.SendDetachedApprovalCard(requestID, target, presentation)
			},
			SendDetachedUserInputCard: func(requestID string, target appclauderuntime.InteractionTarget, payload apppendingforms.ToolUserInputPayload) error {
				return app.bindings.ClaudeSupport.SendDetachedUserInputCard(requestID, target, payload)
			},
			SendDetachedUserInputFormCard: func(requestID string, target appclauderuntime.InteractionTarget, payload apppendingforms.ToolUserInputPayload) error {
				return app.bindings.ClaudeSupport.SendDetachedUserInputFormCard(requestID, target, payload)
			},
			SendDetachedPlanModeCard: func(requestID string, target appclauderuntime.InteractionTarget, body string) error {
				return app.bindings.ClaudeSupport.SendDetachedPlanModeCard(requestID, target, body)
			},
			ExpireInteractionCards: func(sessionKey string, requestIDs []string, reason string) {
				ExpireClaudeInteractionCards(app, sessionKey, requestIDs, reason)
			},
		},
		Lookup: appclauderuntime.LookupDeps{
			FindSubmissionByTurn: func(threadID, turnID string) (string, *domainsubmission.Submission) {
				return findSubmissionByTurn(app, threadID, turnID)
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
				return quietmode.WorkingCardEnabled(app.configView().feishuConfig())
			},
		},
		PrepareClaudeMCPConfig: func(sessionKey string) (string, []string, func(), error) {
			return prepareClaudeMCPConfig(app, sessionKey)
		},
		ModelSettings: func(sessionKey string) domainmodelconfig.Snapshot {
			sess := app.State().Session(app.configView().normalizeSessionKey(sessionKey))
			return modelConfigSnapshot(app, sess, domainbackend.BackendClaude)
		},
		ModelSettingsApplied: func(sessionKey string, settings domainmodelconfig.Snapshot) {
			err := app.bindings.ModelAcknowledgements.Applied(sessionKey, settings)
			if err != nil {
				slog.Warn("record Claude model settings failed", "session_key", sessionKey, "error", err)
			}
		},
		AuxiliaryModels: func(sessionKey string) (string, string) {
			sess := app.State().Session(app.configView().normalizeSessionKey(sessionKey))
			settings := app.bindings.ModelSnapshots.Desired(domainbackend.BackendClaude, sess)
			return settings.SmallModel, settings.SubagentModel
		},
	}
}
