package feishuapp

import (
	"context"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	domainturn "feidex/internal/domain/turn"
	"fmt"
	"log/slog"
	"strings"
	"time"

	codexadapter "feidex/internal/adapter/backend/codex"
	appapproval "feidex/internal/adapter/feishu/approval"
	"feidex/internal/adapter/feishu/claudesupport"
	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	appdelivery "feidex/internal/adapter/feishu/delivery"
	apppendingforms "feidex/internal/adapter/feishu/pendingforms"
	"feidex/internal/adapter/feishu/quietmode"
	appturn "feidex/internal/adapter/feishu/turn"
	"feidex/internal/adapter/feishu/turnmeta"
	appturnstream "feidex/internal/adapter/feishu/turnstream"
	applicationbackendfailure "feidex/internal/application/backendfailure"
	applicationconversation "feidex/internal/application/conversation"
	applicationinteraction "feidex/internal/application/interaction"
	applicationmodelconfig "feidex/internal/application/modelconfig"
	"feidex/internal/application/submission"
	applicationturn "feidex/internal/application/turn"
	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	domainmodelconfig "feidex/internal/domain/modelconfig"
	appclauderuntime "feidex/internal/runtime/claude"
)

type ClaudeRuntimePortInputs struct {
	Runtime               BackendRuntimeDeps
	Config                config.ClaudeConfig
	Cards                 OutboundCardService
	SubmissionLookup      submission.SubmissionLookupService
	ModelSnapshots        applicationmodelconfig.SnapshotService
	ModelAcknowledgements applicationmodelconfig.AcknowledgementService
	ClaudeSupport         *claudesupport.Service
	InteractionLifecycle  applicationinteraction.LifecycleService
	ItemContext           appapproval.ItemContext
	TurnPresentation      *appturnstream.Service
	Turns                 *applicationturn.Service
	BackendFailure        *applicationbackendfailure.BackendFailureService
	Usage                 appdebugviewcmd.UsageService
	TurnMetadata          turnmeta.Service
	ConversationQuery     applicationconversation.Query
	Conversations         *applicationconversation.Service
	Submissions           *submission.SubmissionQueueService
	AsyncRunner           func(func())
}

func ClaudeRuntimePorts(inputs ClaudeRuntimePortInputs) appclauderuntime.Deps {
	runtimeDeps := inputs.Runtime
	runtimeOwner := runtimeDeps.runtime.owner
	stateStore := runtimeDeps.stateView
	submissionLookup := inputs.SubmissionLookup
	modelSnapshots := inputs.ModelSnapshots
	modelAcknowledgements := inputs.ModelAcknowledgements
	claudeSupport := inputs.ClaudeSupport
	interactionLifecycle := inputs.InteractionLifecycle
	itemContext := inputs.ItemContext
	turnPresentation := inputs.TurnPresentation
	turns := inputs.Turns
	backendFailure := inputs.BackendFailure
	usageRecorder := inputs.Usage
	turnMetadata := inputs.TurnMetadata
	conversationQuery := inputs.ConversationQuery
	conversations := inputs.Conversations
	sessionActors := runtimeDeps.sessionActors
	contextFn := runtimeOwner.Lifecycle.Context
	cards := inputs.Cards
	backgroundTasks := claudeBackgroundTaskNotifier{
		client: cards.statusCards.client, state: cards.replyChunks.state,
		frontend: cards.replyChunks.outbound.frontend, runner: cards.replyChunks.outbound.runner,
		ready: cards.replyChunks.ready,
	}
	outputSegments := claudeOutputSegmentDelivery{
		delivery: cards.replyChunks,
		findSubmission: func(threadID, turnID string) (string, *domainsubmission.Submission) {
			return findSubmissionByTurn(submissionLookup, threadID, turnID)
		},
		markStreamFinal: turnPresentation.MarkStreamFinal,
		turnFinalFooter: turnMetadata.TurnFinalFooterLines,
	}
	quietCards := quietWorkingCardExecutor{
		renderer: cards.replyChunks.renderer, state: cards.replyChunks.state, outbound: cards.replyChunks.outbound,
		links: cards.links, turns: turnPresentation, ready: cards.replyChunks.ready,
	}
	return appclauderuntime.Deps{
		Context: contextFn,
		Cfg:     inputs.Config,
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
			SuspendTurnForBackgroundTasks: func(threadID, turnID string) bool {
				sessionKey, _ := findSubmissionByTurn(submissionLookup, threadID, turnID)
				if strings.TrimSpace(sessionKey) == "" {
					return false
				}
				var suspended bool
				runSessionOnActor(sessionActors, sessionKey, func() {
					suspended = turns.SuspendTurnForBackgroundTasks(threadID, turnID)
				})
				return suspended
			},
			FailBackgroundWorkflow: func(sessionKey, threadID, submissionID, message string) {
				runSessionOnActor(sessionActors, sessionKey, func() {
					if inputs.Submissions != nil {
						inputs.Submissions.FailClaudeBackgroundWorkflow(sessionKey, threadID, submissionID, message)
					}
				})
			},
			CancelBackgroundWorkflow: func(sessionKey, threadID, submissionID string) {
				runSessionOnActor(sessionActors, sessionKey, func() {
					if inputs.Submissions != nil {
						inputs.Submissions.CancelClaudeBackgroundWorkflow(sessionKey, threadID, submissionID)
					}
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
				runtimeOwner.TurnBindings.RecordTurnTokenUsage(threadID, turnID, codexadapter.ThreadUsage(usage))
			},
			RecordTurnContextUsagePercent: func(turnID string, percent float64) {
				runtimeOwner.TurnBindings.RecordTurnContextUsagePercent(turnID, percent)
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
				return outputSegments.Update(ctx, threadID, turnID, body, reuseMessageID)
			},
			FinalizeOutputSegment: func(ctx context.Context, threadID, turnID, body string) bool {
				return outputSegments.Finalize(ctx, threadID, turnID, body)
			},
			SendFinalMessages: func(ctx context.Context, sub *domainsubmission.Submission, text string, footerLines []string, inThread bool, reuseMessageIDs []string) []appdelivery.SentReplyChunk {
				return sendFinalMessagesWithFooterAndReuse(cards.replyChunks, ctx, sub, text, footerLines, inThread, reuseMessageIDs)
			},
			ReplyInThread: func(sub *domainsubmission.Submission) bool {
				return replyInThreadForSubmission(sub)
			},
			SendBackgroundTaskNotification: func(ctx context.Context, target appclauderuntime.BackgroundTaskTarget, event claudecli.BackgroundTaskEvent) {
				backgroundTasks.Send(ctx, target, event)
			},
			ContinueBackgroundWorkflow: func(ctx context.Context, sessionKey, threadID, submissionID, prompt string) error {
				if inputs.Submissions == nil {
					return fmt.Errorf("Claude submission service unavailable")
				}
				admitted := runSessionAsync(&runtimeOwner.Lifecycle, inputs.AsyncRunner, sessionActors, sessionKey, func() {
					if err := inputs.Submissions.ContinueClaudeBackgroundWorkflow(sessionKey, threadID, submissionID, prompt); err != nil {
						inputs.Submissions.FailClaudeBackgroundWorkflow(sessionKey, threadID, submissionID, err.Error())
					}
				})
				if !admitted {
					return fmt.Errorf("Claude background workflow continuation could not be scheduled")
				}
				return nil
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
				return stateStore.Session(sessionKey)
			},
			SessionHasActiveOps: func(sess *conversation.Session) bool {
				return conversation.HasActiveOperations(sess)
			},
			SessionHasForegroundOps: func(sess *conversation.Session) bool {
				return conversation.HasForegroundActiveOperations(sess)
			},
			NextLocalID: func(prefix string) (string, error) {
				return stateStore.NextLocalID(prefix)
			},
			WorkspaceCwd: func(workspaceID string) string {
				return workspaceCwd(runtimeDeps.cfg, workspaceID)
			},
		},
		Permission: appclauderuntime.PermissionDeps{
			EffectivePermissionMode: func(sess *conversation.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
				return effectiveBindingClaudePermissionMode(stateStore, sess, ws, cfg)
			},
			QuietWorkingCardEnabled: func() bool {
				return quietmode.WorkingCardEnabled(runtimeDeps.currentBackend().view.feishuConfig())
			},
		},
		PrepareClaudeMCPConfig: func(sessionKey string) (string, []string, func(), error) {
			return runtimeDeps.prepareClaudeMCPConfig(sessionKey)
		},
		ModelSettings: func(sessionKey string) domainmodelconfig.Snapshot {
			view := runtimeDeps.currentBackend().view
			sess := stateStore.Session(view.normalizeSessionKey(sessionKey))
			return modelConfigSnapshot(modelSnapshots, sess, domainbackend.BackendClaude)
		},
		ModelSettingsApplied: func(sessionKey string, settings domainmodelconfig.Snapshot) {
			err := modelAcknowledgements.Applied(sessionKey, settings)
			if err != nil {
				slog.Warn("record Claude model settings failed", "session_key", sessionKey, "error", err)
			}
		},
		AuxiliaryModels: func(sessionKey string) (string, string) {
			view := runtimeDeps.currentBackend().view
			sess := stateStore.Session(view.normalizeSessionKey(sessionKey))
			settings := modelSnapshots.Desired(domainbackend.BackendClaude, sess)
			return settings.SmallModel, settings.SubagentModel
		},
	}
}
