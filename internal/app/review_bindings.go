package app

import (
	"context"

	appcore "feidex/internal/app/appcore"
	appreviewcmd "feidex/internal/app/reviewcmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Adapter methods on *App — satisfy reviewcmd.App interface
// ---------------------------------------------------------------------------

// ReviewFeishu returns the Feishu bot client for the review service.
func (a *App) ReviewFeishu() appcore.FeishuClient {
	return a.feishu
}

// ReviewAppState returns the narrowed app state provider for review ops.
func (a *App) ReviewAppState() appreviewcmd.AppStateProvider {
	return a.State()
}

// ReviewWorkspaceProvider returns the narrowed workspace provider for review
// ops.
func (a *App) ReviewWorkspaceProvider() appreviewcmd.WorkspaceProvider {
	return reviewWorkspaceProviderAdapter{app: a}
}

// ReviewGitProvider returns the narrowed git provider for review ops.
func (a *App) ReviewGitProvider() appreviewcmd.ReviewGitProvider {
	return newReviewGitService(a)
}

// ReviewCodexClient returns the current Codex RPC client for the review
// service.
func (a *App) ReviewCodexClient() (appreviewcmd.CodexClient, error) {
	return requireCodexClient(a)
}

// ReviewMakeSessionKey builds a session key from an inbound message.
func (a *App) ReviewMakeSessionKey(msg *feishu.InboundMessage) string {
	return makeSessionKey(a, msg)
}

// ReviewReplyInThreadEnabled reports whether reply-in-thread is enabled for
// the given chat type.
func (a *App) ReviewReplyInThreadEnabled(chatType string) bool {
	return replyInThreadEnabled(a, chatType)
}

// ReviewMenuCardBody formats a menu card body with breadcrumb navigation.
func (a *App) ReviewMenuCardBody(action, body string) string {
	return menuCardBody(action, body)
}

// ReviewActionStringValue extracts a string value from a card action.
func (a *App) ReviewActionStringValue(action *feishu.CardAction, key string) string {
	return actionStringValue(action, key)
}

// ReviewCommandMessageFromAction builds an InboundMessage from a card action.
func (a *App) ReviewCommandMessageFromAction(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage {
	return commandMessageFromAction(a, action, sessionKey, rawCommand)
}

// ReviewSessionHasActiveWork reports whether the session has active work.
func (a *App) ReviewSessionHasActiveWork(sess *state.Session) bool {
	return sessionHasActiveWork(sess)
}

// ReviewSessionHasInFlightSubmission reports whether the session has an
// in-flight submission.
func (a *App) ReviewSessionHasInFlightSubmission(sess *state.Session) bool {
	return sessionHasInFlightSubmission(sess)
}

// ReviewStartNextSubmission starts the next queued submission for the given
// session.
func (a *App) ReviewStartNextSubmission(sessionKey string) error {
	return startNextSubmission(a, sessionKey)
}

// ReviewSendSubmissionQueuedNotice sends a queued notice for the submission.
func (a *App) ReviewSendSubmissionQueuedNotice(ctx context.Context, sub *state.Submission) {
	sendSubmissionQueuedNotice(a, ctx, sub)
}

// ReviewMarkSubmissionQueuedReactions marks the submission with queued
// reactions.
func (a *App) ReviewMarkSubmissionQueuedReactions(sub *state.Submission) {
	newPendingQueueService(a).markSubmissionQueuedReactions(sub)
}

// ReviewCompleteAsyncRenderedCardAction runs an action asynchronously and
// patches the card.
func (a *App) ReviewCompleteAsyncRenderedCardAction(
	action *feishu.CardAction,
	sessionKey, toastText string,
	preparingCard map[string]any,
	run func() (*callback.CardActionTriggerResponse, error),
	failureCard func(sessionKey, errText string) map[string]any,
	patchWarnMsg string,
) (*callback.CardActionTriggerResponse, error) {
	return completeAsyncRenderedCardAction(a, action, sessionKey, toastText, preparingCard, run, failureCard, patchWarnMsg)
}

// ReviewRenderPreparingCard renders a preparing card for review operations.
func (a *App) ReviewRenderPreparingCard(sessionKey, body string) map[string]any {
	return renderReviewPreparingCard(a, sessionKey, body)
}

// ReviewRenderFailureCard renders a failure card for review operations.
func (a *App) ReviewRenderFailureCard(sessionKey, errText, retryAction string) map[string]any {
	return renderReviewFailureCard(a, sessionKey, errText, retryAction)
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
