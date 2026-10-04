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
	// The ports are built by a runtime factory, so these are read here rather
	// than through app.bindings inside the callbacks below.
	submissionLookup := app.bindings.SubmissionLookup
	modelSnapshots := app.bindings.ModelSnapshots
	modelAcknowledgements := app.bindings.ModelAcknowledgements
	claudeSupport := app.bindings.ClaudeSupport
	interactionLifecycle := app.bindings.InteractionLifecycle
	itemContext := app.bindings.ItemContext
	turnPresentation := app.bindings.TurnPresentation
	turns := app.bindings.Turns
	backendFailure := app.bindings.BackendFailure
	usageRecorder := app.bindings.Usage
	turnMetadata := app.bindings.TurnMetadata
	stateStore := app.State()
	conversationQuery := app.bindings.ConversationQuery
	conversations := app.bindings.Conversations
	sessionActors := app.runtimeOwner.SessionActors
	runtimeDeps := app.BackendRuntimeDeps()
	contextFn := app.runtimeOwner.Lifecycle.Context
	cards := newOutboundCardService(app)
	quietCards := quietWorkingCardExecutor{
		renderer: cards.replyChunks.renderer, state: cards.replyChunks.state, outbound: cards.replyChunks.outbound,
		links: cards.links, turns: turnPresentation, ready: cards.replyChunks.ready,
	}
	return appclauderuntime.Deps{
		Context: contextFn,
		Cfg:     cfg,
		Lifecycle: appclauderuntime.LifecycleDeps{
			BindClaudeSessionThread: func(sessionKey, turnID, threadID string) {
				runSessionOnActor(sessionActors, sessionKey, func() {
					bindClaudeSessionThread(conversations, sessionKey, turnID, threadID)
				})
			},
			FinishTurn: func(threadID, turnID, status string) {
				sessionKey := conversationQuery.SessionForBackendThread(threadID)
				runSessionOnActor(sessionActors, sessionKey, func() {
					finishTurn(turns, threadID, turnID, status)
				})
			},
			FinishSteerSubmission: func(submissionID, status string) {
				sessionKey := ""
				if sub := stateStore.Submission(submissionID); sub != nil {
					sessionKey = sub.SessionKey
				}
				runSessionOnActor(sessionActors, sessionKey, func() {
					turns.FinishSteerSubmission(submissionID, status)
				})
			},
			FailClaudeSessionWork: func(sessionKey, threadID string, err error) {
				runSessionOnActor(sessionActors, sessionKey, func() {
					failClaudeSessionActiveWork(backendFailure, sessionKey, threadID, err)
				})
			},
			FailBackendActiveWork: func(backend, sessionKey, threadID, message string) {
				failBackendActiveWork(runtimeDeps.currentBackend(), backend, sessionKey, threadID, message)
			},
		},
		TurnStream: appclauderuntime.TurnStreamDeps{
			Port: claudeTurnStreamPort{itemContext: itemContext, turnPresentation: turnPresentation},
		},
		Usage: appclauderuntime.UsageDeps{
			RecordClaudeThreadUsage: func(threadID string, usage claudecli.TurnUsage) {
				usageRecorder.RecordClaudeThreadUsage(threadID, domainturn.ClaudeThreadUsage{
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
				return turnMetadata.TurnFinalFooterLines(turnID, completedAt)
			},
		},
		Delivery: appclauderuntime.DeliveryDeps{
			ExecuteQuietWorkingCardOp: func(ctx context.Context, sub *domainsubmission.Submission, op appturn.QuietWorkingCardOp) {
				quietCards.ExecuteQuietWorkingCardOp(ctx, sub, op)
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
				return replyInThreadForSubmission(sub)
			},
			SendBackgroundTaskNotification: func(ctx context.Context, target appclauderuntime.BackgroundTaskTarget, event claudecli.BackgroundTaskEvent) {
				sendClaudeBackgroundTaskNotification(app, ctx, target, event)
			},
		},
		Interactive: appclauderuntime.InteractiveDeps{
			SendClaudeApprovalCard: func(requestID, sessionKey string, sub *domainsubmission.Submission, presentation appapproval.Presentation) error {
				return sendClaudeApprovalCard(claudeSupport, requestID, sessionKey, sub, presentation)
			},
			SendClaudeUserInputCard: func(requestID, sessionKey string, sub *domainsubmission.Submission, payload apppendingforms.ToolUserInputPayload) error {
				return sendClaudeUserInputCard(claudeSupport, requestID, sessionKey, sub, payload)
			},
			SendClaudeUserInputFormCard: func(requestID, sessionKey string, sub *domainsubmission.Submission, payload apppendingforms.ToolUserInputPayload) error {
				return sendClaudeUserInputFormCard(claudeSupport, requestID, sessionKey, sub, payload)
			},
			SendClaudePlanModeCard: func(requestID, sessionKey string, sub *domainsubmission.Submission, threadID, turnID, body string) error {
				return sendClaudePlanModeCard(claudeSupport, requestID, sessionKey, sub, threadID, turnID, body)
			},
			SendDetachedApprovalCard: func(requestID string, target appclauderuntime.InteractionTarget, presentation appapproval.Presentation) error {
				return claudeSupport.SendDetachedApprovalCard(requestID, target, presentation)
			},
			SendDetachedUserInputCard: func(requestID string, target appclauderuntime.InteractionTarget, payload apppendingforms.ToolUserInputPayload) error {
				return claudeSupport.SendDetachedUserInputCard(requestID, target, payload)
			},
			SendDetachedUserInputFormCard: func(requestID string, target appclauderuntime.InteractionTarget, payload apppendingforms.ToolUserInputPayload) error {
				return claudeSupport.SendDetachedUserInputFormCard(requestID, target, payload)
			},
			SendDetachedPlanModeCard: func(requestID string, target appclauderuntime.InteractionTarget, body string) error {
				return claudeSupport.SendDetachedPlanModeCard(requestID, target, body)
			},
			ExpireInteractionCards: func(sessionKey string, requestIDs []string, reason string) {
				ExpireClaudeInteractionCards(interactionLifecycle, sessionKey, requestIDs, reason)
			},
		},
		Lookup: appclauderuntime.LookupDeps{
			FindSubmissionByTurn: func(threadID, turnID string) (string, *domainsubmission.Submission) {
				return findSubmissionByTurn(submissionLookup, threadID, turnID)
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
				return effectiveBindingClaudePermissionMode(app.State(), sess, ws, cfg)
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
			return modelConfigSnapshot(modelSnapshots, sess, domainbackend.BackendClaude)
		},
		ModelSettingsApplied: func(sessionKey string, settings domainmodelconfig.Snapshot) {
			err := modelAcknowledgements.Applied(sessionKey, settings)
			if err != nil {
				slog.Warn("record Claude model settings failed", "session_key", sessionKey, "error", err)
			}
		},
		AuxiliaryModels: func(sessionKey string) (string, string) {
			sess := app.State().Session(app.configView().normalizeSessionKey(sessionKey))
			settings := modelSnapshots.Desired(domainbackend.BackendClaude, sess)
			return settings.SmallModel, settings.SubagentModel
		},
	}
}
