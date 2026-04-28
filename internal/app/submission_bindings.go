package app

import (
	"context"

	appconvbackend "feidex/internal/app/convbackend"
	appsubmission "feidex/internal/app/submission"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type sqLiveThreadAdapter struct{ app *App }

func (a sqLiveThreadAdapter) MarkSessionThreadLive(sessionKey, threadID string) {
	markSessionThreadLive(a.app, sessionKey, threadID)
}
func (a sqLiveThreadAdapter) SessionHasLiveThread(sessionKey, threadID string) bool {
	return sessionHasLiveThread(a.app, sessionKey, threadID)
}
func (a sqLiveThreadAdapter) ClearSessionLiveThread(sessionKey string) {
	clearSessionLiveThread(a.app, sessionKey)
}

type sqConversationBackendAdapter struct {
	facade appconvbackend.ConversationBackendFacade
}

func (a sqConversationBackendAdapter) StartQueuedSubmission(sessionKey string, sess *state.Session, sub *state.Submission, ws *config.Workspace, notifyFailure bool) error {
	if a.facade == nil {
		return nil
	}
	return a.facade.StartQueuedSubmission(sessionKey, sess, sub, ws, notifyFailure)
}

// ---------------------------------------------------------------------------
// App adapter — implements submission.App for *App
// ---------------------------------------------------------------------------

type submissionAppAdapter struct{ app *App }

func (a submissionAppAdapter) SubmissionQueueAppState() appsubmission.QueueAppStateProvider {
	return a.app.State()
}
func (a submissionAppAdapter) SubmissionQueueSkillResolver() appsubmission.QueueSkillResolver {
	return sqSkillResolverAdapter{app: a.app}
}
func (a submissionAppAdapter) SubmissionQueueAttachmentResolver() appsubmission.QueueAttachmentResolver {
	return sqAttachmentResolverFullAdapter{app: a.app}
}
func (a submissionAppAdapter) SubmissionQueueLiveThread() appsubmission.QueueLiveThreadProvider {
	return sqLiveThreadAdapter{app: a.app}
}
func (a submissionAppAdapter) SubmissionQueuePendingQueue() appsubmission.QueuePendingQueueProvider {
	return sqPendingQueueFullAdapter{app: a.app}
}
func (a submissionAppAdapter) SubmissionQueueRuntimeState() appsubmission.QueueRuntimeStateProvider {
	return newRuntimeStateService(a.app)
}
func (a submissionAppAdapter) SubmissionQueueRuntimeMaintenance() appsubmission.QueueRuntimeMaintenanceProvider {
	return newRuntimeMaintenanceService(a.app)
}
func (a submissionAppAdapter) SubmissionQueueReplyContinuation() appsubmission.QueueReplyContinuationProvider {
	return newReplyContinuationService(a.app)
}
func (a submissionAppAdapter) SubmissionQueueTurnStream() appsubmission.QueueTurnStreamProvider {
	return newTurnStreamService(a.app)
}
func (a submissionAppAdapter) SubmissionQueueAutoRetry() appsubmission.QueueAutoRetryProvider {
	return newAutoRetryService(a.app)
}
func (a submissionAppAdapter) SubmissionQueueConversationBackend() appsubmission.QueueConversationBackendProvider {
	return sqConversationBackendAdapter{facade: conversationBackend(a.app)}
}
func (a submissionAppAdapter) SubmissionQueueBackendRuntime() appsubmission.QueueBackendRuntimeProvider {
	return sqBackendRuntimeFullAdapter{app: a.app}
}
func (a submissionAppAdapter) SubmissionQueueDefaultWorkspaceID() string {
	return defaultWorkspaceID(a.app)
}
func (a submissionAppAdapter) SubmissionQueueWorkspace(id string) *config.Workspace {
	return config.FindWorkspace(a.app.cfg, id)
}
func (a submissionAppAdapter) SubmissionQueueReplyInThreadEnabled(chatType string) bool {
	return replyInThreadEnabled(a.app, chatType)
}
func (a submissionAppAdapter) SubmissionQueueReplyInThreadForSubmission(sub *state.Submission) bool {
	return replyInThreadForSubmission(a.app, sub)
}
func (a submissionAppAdapter) SubmissionQueueConfiguredInflightMode() appsubmission.QueueInflightMode {
	return inflightModeToInt(configuredSessionInflightMode(a.app))
}
func (a submissionAppAdapter) SubmissionQueueInflightAllowsAdditional(mode appsubmission.QueueInflightMode) bool {
	return sessionInflightAllowsAdditional(intToInflightMode(mode))
}

func inflightModeToInt(m sessionInflightMode) appsubmission.QueueInflightMode {
	switch m {
	case "serialized":
		return 1
	case "parallel":
		return 2
	default:
		return 0
	}
}

func intToInflightMode(m appsubmission.QueueInflightMode) sessionInflightMode {
	switch m {
	case 1:
		return "serialized"
	case 2:
		return "parallel"
	default:
		return "single"
	}
}
func (a submissionAppAdapter) SubmissionQueueReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	return a.app.feishu.ReplyText(ctx, messageID, text, inThread)
}
func (a submissionAppAdapter) SubmissionQueueSendQueuedNotice(ctx context.Context, sub *state.Submission) {
	sendSubmissionQueuedNotice(a.app, ctx, sub)
}
func (a submissionAppAdapter) SubmissionQueueSendStartFailureNotice(ctx context.Context, sub *state.Submission, err error, willContinue bool) {
	// Delegates to the coordinator method.
	newSubmissionCoordinator(a.app).notifySubmissionStartFailure(ctx, sub, err, willContinue)
}
func (a submissionAppAdapter) SubmissionQueueRunAsync(fn func()) {
	runAsync(a.app, fn)
}
func (a submissionAppAdapter) SubmissionQueueLogSessionState(event, sessionKey string, sess *state.Session) {
	logSessionState(event, sessionKey, sess)
}
func (a submissionAppAdapter) SubmissionQueueMarkSubmissionQueuedReactions(sub *state.Submission) {
	newPendingQueueService(a.app).markSubmissionQueuedReactions(sub)
}
func (a submissionAppAdapter) SubmissionQueueMarkSubmissionRunningReactions(sub *state.Submission) {
	newPendingQueueService(a.app).markSubmissionRunningReactions(sub)
}
func (a submissionAppAdapter) SubmissionQueueClearSubmissionProcessingReactions(sub *state.Submission) {
	newPendingQueueService(a.app).clearSubmissionProcessingReactions(sub)
}
func (a submissionAppAdapter) SubmissionQueueIsReviewSubmission(sub *state.Submission) bool {
	return isReviewSubmission(sub)
}
func (a submissionAppAdapter) SubmissionQueueStartSubmissionTurn(ctx context.Context, sessionKey, threadID string, sub *state.Submission, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort string) (string, error) {
	return startSubmissionTurn(a.app, ctx, sessionKey, threadID, sub, cwd, approvalPolicy, sandboxMode, serviceTier, model, reasoningEffort)
}
func (a submissionAppAdapter) SubmissionQueueStartSubmissionReview(ctx context.Context, threadID string, sub *state.Submission) (string, error) {
	return startSubmissionReview(a.app, ctx, threadID, sub)
}
func (a submissionAppAdapter) SubmissionQueueBuildThreadStartParams(ws *config.Workspace, sess *state.Session, model string) codexrpc.ThreadStartParams {
	return buildThreadStartParams(a.app, ws, sess, model)
}
func (a submissionAppAdapter) SubmissionQueueRequireCodexClient() (CodexClient, error) {
	return requireCodexClient(a.app)
}

// ---------------------------------------------------------------------------
// Full adapter types for providers that need app access
// ---------------------------------------------------------------------------

type sqSkillResolverAdapter struct{ app *App }

func (a sqSkillResolverAdapter) ResolveSubmissionSkill(sessionKey, workspaceID, inputText string, attachments []state.SubmissionAttachment) appsubmission.QueueSkillResolution {
	resolution := newSkillsService(a.app).ResolveSubmissionSkill(sessionKey, workspaceID, inputText, attachments)
	return appsubmission.QueueSkillResolution{
		InputText:          resolution.InputText,
		Skills:             resolution.Skills,
		ConsumePending:     resolution.ConsumePending,
		PendingReplacement: resolution.PendingReplacement,
	}
}
func (a sqSkillResolverAdapter) SetSessionPendingSkill(sessionKey string, skill state.SubmissionSkill) {
	newSkillsService(a.app).SetSessionPendingSkill(sessionKey, skill)
}
func (a sqSkillResolverAdapter) ClearSessionPendingSkill(sessionKey string) {
	newSkillsService(a.app).ClearSessionPendingSkill(sessionKey)
}

type sqAttachmentResolverFullAdapter struct{ app *App }

func (a sqAttachmentResolverFullAdapter) ResolveInboundAttachments(msg *feishu.InboundMessage, workspaceID, sessionKey string) ([]state.SubmissionAttachment, error) {
	return resolveInboundAttachments(a.app, msg, workspaceID, sessionKey)
}

type sqPendingQueueFullAdapter struct{ app *App }

func (a sqPendingQueueFullAdapter) PendingInputSessionKey(msg *feishu.InboundMessage) string {
	return newReplyContinuationService(a.app).pendingInputSessionKey(msg)
}
func (a sqPendingQueueFullAdapter) CollectPendingStagedImages(sessionKey, bucketSessionKey string) []state.SessionStagedImage {
	return newReplyContinuationService(a.app).collectPendingStagedImages(sessionKey, bucketSessionKey)
}
func (a sqPendingQueueFullAdapter) ClearPendingStagedImages(sessionKey, bucketSessionKey string) error {
	return newReplyContinuationService(a.app).clearPendingStagedImages(sessionKey, bucketSessionKey)
}

type sqBackendRuntimeFullAdapter struct{ app *App }

func (a sqBackendRuntimeFullAdapter) ReconcileCompletedTurnFromFinalOutput(sessionKey string, sess *state.Session) *state.Session {
	if runtime := backendRuntime(a.app); runtime != nil {
		return runtime.reconcileCompletedTurnFromFinalOutput(a.app, sessionKey, sess)
	}
	return sess
}
func (a sqBackendRuntimeFullAdapter) DropThreadLineageAfterStartFailure(err error) bool {
	if runtime := backendRuntime(a.app); runtime != nil {
		return runtime.dropThreadLineageAfterStartFailure(a.app, err)
	}
	return false
}
func (a sqBackendRuntimeFullAdapter) DeferQueuedSubmissionsDuringRecovery() bool {
	if runtime := backendRuntime(a.app); runtime != nil {
		return runtime.deferQueuedSubmissionsDuringRecovery(a.app)
	}
	return false
}

// ---------------------------------------------------------------------------
// PendingQueueApp adapter — implements submission.PendingQueueApp for *App
// ---------------------------------------------------------------------------

type pendingQueueAppAdapter struct{ app *App }

func (a pendingQueueAppAdapter) PendingQueueAppState() appsubmission.PendingQueueAppStateProvider {
	return a.app.State()
}
func (a pendingQueueAppAdapter) PendingQueueRuntimeMaintenance() appsubmission.PendingQueueRuntimeMaintenanceProvider {
	return newRuntimeMaintenanceService(a.app)
}
func (a pendingQueueAppAdapter) PendingQueueDefaultWorkspaceID() string {
	return defaultWorkspaceID(a.app)
}
func (a pendingQueueAppAdapter) PendingQueueAddReaction(ctx context.Context, messageID, emoji string) error {
	return a.app.feishu.AddReaction(ctx, messageID, emoji)
}
func (a pendingQueueAppAdapter) PendingQueueRemoveReaction(ctx context.Context, messageID, emoji string) error {
	return a.app.feishu.RemoveReaction(ctx, messageID, emoji)
}
func (a pendingQueueAppAdapter) PendingQueueLogSessionState(event, sessionKey string, sess *state.Session) {
	logSessionState(event, sessionKey, sess)
}

// ---------------------------------------------------------------------------
// Convenience constructors
// ---------------------------------------------------------------------------

func newSubmissionQueueServiceFromApp(a *App) appsubmission.SubmissionQueueService {
	return appsubmission.NewSubmissionQueueService(submissionAppAdapter{app: a})
}

func newPendingQueueServiceFromApp(a *App) appsubmission.PendingQueueService {
	return appsubmission.NewPendingQueueService(pendingQueueAppAdapter{app: a})
}
