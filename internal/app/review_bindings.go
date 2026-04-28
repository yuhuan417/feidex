package app

import (
	"context"

	appcore "feidex/internal/app/appcore"
	appreview "feidex/internal/app/review"
	appreviewcmd "feidex/internal/app/reviewcmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Type and constant aliases — reviewcmd exported types
// ---------------------------------------------------------------------------

type reviewPendingPayload = appreviewcmd.ReviewPendingPayload

const (
	submissionKindReview = appreviewcmd.SubmissionKindReview
	pendingKindReview    = appreviewcmd.PendingKindReview

	reviewFormModeBase   = appreviewcmd.ReviewFormModeBase
	reviewFormModeCommit = appreviewcmd.ReviewFormModeCommit
	reviewFormModeCustom = appreviewcmd.ReviewFormModeCustom
)

func reviewPendingPayloadFromPending(pending *state.PendingRequest) reviewPendingPayload {
	return appreviewcmd.ReviewPendingPayloadFromPending(pending)
}

// ---------------------------------------------------------------------------
// App adapters — satisfy reviewcmd.App without adding feature methods on *App
// ---------------------------------------------------------------------------

type reviewAppAdapter struct{ *App }

func newReviewAppAdapter(a *App) reviewAppAdapter {
	return reviewAppAdapter{App: a}
}

func newReviewFormServiceInner(app *App) appreviewcmd.ReviewFormService {
	return appreviewcmd.NewReviewFormService(newReviewAppAdapter(app))
}

func (a reviewAppAdapter) ReviewFeishu() appcore.FeishuClient {
	return a.feishu
}

func (a reviewAppAdapter) ReviewAppState() appreviewcmd.AppStateProvider {
	return a.State()
}

func (a reviewAppAdapter) ReviewWorkspaceProvider() appreviewcmd.WorkspaceProvider {
	return reviewWorkspaceProviderAdapter{app: a.App}
}

func (a reviewAppAdapter) ReviewGitProvider() appreviewcmd.ReviewGitProvider {
	return reviewGitProviderAdapter{}
}

func (a reviewAppAdapter) ReviewCodexClient() (appreviewcmd.CodexClient, error) {
	return requireCodexClient(a.App)
}

func (a reviewAppAdapter) ReviewMakeSessionKey(msg *feishu.InboundMessage) string {
	return makeSessionKey(a.App, msg)
}

func (a reviewAppAdapter) ReviewReplyInThreadEnabled(chatType string) bool {
	return replyInThreadEnabled(a.App, chatType)
}

func (a reviewAppAdapter) ReviewMenuCardBody(action, body string) string {
	return menuCardBody(action, body)
}

func (a reviewAppAdapter) ReviewActionStringValue(action *feishu.CardAction, key string) string {
	return actionStringValue(action, key)
}

func (a reviewAppAdapter) ReviewCommandMessageFromAction(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage {
	return commandMessageFromAction(a.App, action, sessionKey, rawCommand)
}

func (a reviewAppAdapter) ReviewSessionHasActiveWork(sess *state.Session) bool {
	return sessionHasActiveWork(sess)
}

func (a reviewAppAdapter) ReviewSessionHasInFlightSubmission(sess *state.Session) bool {
	return sessionHasInFlightSubmission(sess)
}

func (a reviewAppAdapter) ReviewStartNextSubmission(sessionKey string) error {
	return startNextSubmission(a.App, sessionKey)
}

func (a reviewAppAdapter) ReviewSendSubmissionQueuedNotice(ctx context.Context, sub *state.Submission) {
	sendSubmissionQueuedNotice(a.App, ctx, sub)
}

func (a reviewAppAdapter) ReviewMarkSubmissionQueuedReactions(sub *state.Submission) {
	newPendingQueueService(a.App).markSubmissionQueuedReactions(sub)
}

func (a reviewAppAdapter) ReviewCompleteAsyncCommandAction(
	action *feishu.CardAction,
	sessionKey, rawCommand, fallbackAction, toastText string,
	preparingCard map[string]any,
	successCardFromText func(sessionKey, text string) map[string]any,
	failureCard func(sessionKey, errText string) map[string]any,
	patchWarnMsg string,
) (*callback.CardActionTriggerResponse, error) {
	return completeAsyncCommandAction(a.App, action, sessionKey, rawCommand, fallbackAction, toastText, preparingCard, successCardFromText, failureCard, patchWarnMsg)
}

func (a reviewAppAdapter) ReviewCompleteAsyncRenderedCardAction(
	action *feishu.CardAction,
	sessionKey, toastText string,
	preparingCard map[string]any,
	run func() (*callback.CardActionTriggerResponse, error),
	failureCard func(sessionKey, errText string) map[string]any,
	patchWarnMsg string,
) (*callback.CardActionTriggerResponse, error) {
	return completeAsyncRenderedCardAction(a.App, action, sessionKey, toastText, preparingCard, run, failureCard, patchWarnMsg)
}

// ---------------------------------------------------------------------------
// Internal adapter types
// ---------------------------------------------------------------------------

// reviewWorkspaceProviderAdapter wraps workspace access for the review
// service.
type reviewWorkspaceProviderAdapter struct {
	app *App
}

func (a reviewWorkspaceProviderAdapter) ReviewWorkspaceForSessionKey(sessionKey string) *config.Workspace {
	sess := a.app.State().Session(sessionKey)
	workspaceID := defaultWorkspaceID(a.app)
	if sess != nil {
		if wid := sess.WorkspaceID; wid != "" {
			workspaceID = wid
		}
	}
	return config.FindWorkspace(a.app.cfg, workspaceID)
}

func (a reviewWorkspaceProviderAdapter) ReviewDefaultWorkspaceID() string {
	return defaultWorkspaceID(a.app)
}

func (a reviewWorkspaceProviderAdapter) ReviewFindWorkspace(workspaceID string) *config.Workspace {
	return config.FindWorkspace(a.app.cfg, workspaceID)
}

type reviewGitProviderAdapter struct{}

func (reviewGitProviderAdapter) ReviewResolveTarget(cwd string, target appreview.TargetSpec) (appreview.TargetSpec, error) {
	return appreview.NewGitService().ResolveTarget(cwd, target)
}

func (reviewGitProviderAdapter) ReviewListBranches(cwd string) ([]appreview.BranchOption, error) {
	return appreview.NewGitService().ListBranches(cwd)
}

func (reviewGitProviderAdapter) ReviewListCommits(cwd string, limit int) ([]appreview.CommitOption, error) {
	return appreview.NewGitService().ListCommits(cwd, limit)
}
