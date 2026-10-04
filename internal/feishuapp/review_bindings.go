package feishuapp

import (
	"context"
	reviewapp "feidex/internal/application/review"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"

	appreview "feidex/internal/adapter/feishu/review"

	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Type and constant aliases — reviewcmd exported types
// ---------------------------------------------------------------------------

const (
	submissionKindReview = appreviewcmd.SubmissionKindReview
	pendingKindReview    = appreviewcmd.PendingKindReview

	reviewFormModeBase   = appreviewcmd.ReviewFormModeBase
	reviewFormModeCommit = appreviewcmd.ReviewFormModeCommit
	reviewFormModeCustom = appreviewcmd.ReviewFormModeCustom
)

func reviewPendingPayloadFromPending(pending *state.PendingRequest) appreviewcmd.ReviewPendingPayload {
	return appreviewcmd.ReviewPendingPayloadFromPending(pending)
}

// ---------------------------------------------------------------------------
// App adapters — satisfy reviewcmd.App without adding feature methods on *App
// ---------------------------------------------------------------------------

func newReviewAppAdapter(a *App) appreviewcmd.Dependencies {
	if a == nil {
		return appreviewcmd.Dependencies{}
	}
	return appreviewcmd.Dependencies{
		UseCase:        a.bindings.Review,
		ConfigProvider: a, Outbound: newEffectOutbound(a.FrontendID(), newEffectRunner(a.runtimeOwner)), CardRenderer: simpleStatusCardRenderer{client: a.feishu}, StateProvider: a.State(),
		ContextProvider:        a,
		WorkspaceProviderValue: reviewWorkspaceProviderAdapter{app: a}, GitProvider: reviewGitProviderAdapter{app: a},
		CodexClientFn:    func() (appreviewcmd.CodexClient, error) { return requireCodexGateway(a) },
		MakeSessionKeyFn: func(m *feishu.InboundMessage) string { return a.configView().makeSessionKey(m) }, ReplyInThreadEnabledFn: func(v string) bool { return a.configView().replyInThreadEnabled() },
		MenuCardBodyFn: menuCardBody, ActionStringValueFn: actionStringValue,
		CommandMessageFromActionFn: func(x *feishu.CardAction, s, r string) *feishu.InboundMessage {
			return commandMessageFromAction(a, x, s, r)
		},
		SessionHasActiveWorkFn: sessionHasActiveWork, SessionHasInFlightSubmissionFn: conversation.HasInFlightSubmission,
		StartNextSubmissionFn:           func(s string) error { return startNextSubmission(a.bindings.Submissions, s) },
		SendSubmissionQueuedNoticeFn:    func(c context.Context, s *domainsubmission.Submission) { sendSubmissionQueuedNotice(a, c, s) },
		MarkSubmissionQueuedReactionsFn: func(s *domainsubmission.Submission) { a.bindings.PendingQueue.MarkSubmissionQueuedReactions(s) },
		CompleteAsyncCommandActionFn: func(x *feishu.CardAction, s, r, f, t string, p map[string]any, ok, fail func(string, string) map[string]any, w string) (*callback.CardActionTriggerResponse, error) {
			return completeAsyncCommandAction(a, x, s, r, f, t, p, ok, fail, w)
		},
		CompleteAsyncRenderedCardActionFn: func(x *feishu.CardAction, s, t string, p map[string]any, r func() (*callback.CardActionTriggerResponse, error), f func(string, string) map[string]any, w string) (*callback.CardActionTriggerResponse, error) {
			return completeAsyncRenderedCardAction(a, x, s, t, p, r, f, w)
		},
	}
}

func BuildReviewCommands(app *App) appreviewcmd.ReviewFormService {
	return appreviewcmd.NewReviewFormService(newReviewAppAdapter(app))
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
	workspaceID := a.app.configView().defaultWorkspaceID()
	if sess != nil {
		if wid := sess.WorkspaceID; wid != "" {
			workspaceID = wid
		}
	}
	return config.FindWorkspace(a.app.cfg, workspaceID)
}

func (a reviewWorkspaceProviderAdapter) ReviewDefaultWorkspaceID() string {
	return a.app.configView().defaultWorkspaceID()
}

func (a reviewWorkspaceProviderAdapter) ReviewFindWorkspace(workspaceID string) *config.Workspace {
	return config.FindWorkspace(a.app.cfg, workspaceID)
}

type reviewGitProviderAdapter struct{ app *App }

func (a reviewGitProviderAdapter) ReviewResolveTarget(cwd string, target appreview.TargetSpec) (appreview.TargetSpec, error) {
	return (appreview.GitService{Context: a.app.Context()}).ResolveTarget(cwd, target)
}

func (a reviewGitProviderAdapter) ReviewListBranches(cwd string) ([]appreview.BranchOption, error) {
	return (appreview.GitService{Context: a.app.Context()}).ListBranches(cwd)
}

func (a reviewGitProviderAdapter) ReviewListCommits(cwd string, limit int) ([]appreview.CommitOption, error) {
	return (appreview.GitService{Context: a.app.Context()}).ListCommits(cwd, limit)
}

type reviewTargetResolver struct{ app *App }

func (r reviewTargetResolver) Resolve(cwd string, target appreview.TargetSpec) (appreview.TargetSpec, error) {
	return (appreview.GitService{Context: r.app.Context()}).ResolveTarget(cwd, target)
}

type reviewDispatcher struct{ app *App }

func (d reviewDispatcher) StartNext(key string) error {
	return d.app.bindings.Submissions.StartNextSubmission(key)
}
func (d reviewDispatcher) MarkQueued(sub *domainsubmission.Submission) {
	d.app.bindings.PendingQueue.MarkSubmissionQueuedReactions(sub)
}
func (d reviewDispatcher) Notify(ctx context.Context, sub *domainsubmission.Submission) {
	sendSubmissionQueuedNotice(d.app, ctx, sub)
}
func ReviewPorts(a *App) reviewapp.Dependencies {
	return reviewapp.Dependencies{Forms: a.bindings.Forms, Delivery: a.bindings.InteractionDelivery, Options: reviewOptions{app: a}, Gateway: func() (reviewapp.Gateway, error) { return requireCodexGateway(a) }, Repository: a.State(), Resolver: reviewTargetResolver{app: a}, Dispatcher: reviewDispatcher{app: a}}
}

type reviewOptions struct{ app *App }

func (o reviewOptions) Branches(cwd string) ([]reviewapp.BranchOption, error) {
	return (appreview.GitService{Context: o.app.Context()}).ListBranches(cwd)
}
func (o reviewOptions) Commits(cwd string, limit int) ([]reviewapp.CommitOption, error) {
	return (appreview.GitService{Context: o.app.Context()}).ListCommits(cwd, limit)
}
