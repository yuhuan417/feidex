package feishuapp

import (
	"context"
	"feidex/internal/application/interaction"
	reviewapp "feidex/internal/application/review"
	appsubmission "feidex/internal/application/submission"
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
	submissions := a.bindings.Submissions
	bindingScope := a.bindings.BindingCommands.scope
	pendingQueue := a.bindings.PendingQueue
	queuedNotice := a.bindings.OutboundCards
	configProvider := newFrontendConfigProvider(a.BackendRuntimeDeps(), a.store, a.bindings.WorkspaceSelection)
	return appreviewcmd.Dependencies{
		UseCase:        a.bindings.Review,
		ConfigProvider: configProvider, Outbound: newEffectOutbound(a.FrontendID(), newEffectRunner(a.runtimeOwner)), CardRenderer: simpleStatusCardRenderer{client: a.feishu}, StateProvider: a.State(),
		ContextProvider:        a,
		WorkspaceProviderValue: reviewWorkspaceProviderAdapter{config: a.Config(), configView: a.configView(), session: a.State().Session}, GitProvider: reviewGitProviderAdapter{context: a.runtimeOwner.Lifecycle.Context},
		CodexClientFn:    func() (appreviewcmd.CodexClient, error) { return a.runtimeView().requireCodexGateway() },
		MakeSessionKeyFn: func(m *feishu.InboundMessage) string { return a.configView().makeSessionKey(m) }, ReplyInThreadEnabledFn: func(v string) bool { return a.configView().replyInThreadEnabled() },
		MenuCardBodyFn: menuCardBody, ActionStringValueFn: actionStringValue,
		CommandMessageFromActionFn: func(x *feishu.CardAction, s, r string) *feishu.InboundMessage {
			return commandMessageFromAction(bindingScope, x, s, r)
		},
		SessionHasActiveWorkFn: sessionHasActiveWork, SessionHasInFlightSubmissionFn: conversation.HasInFlightSubmission,
		StartNextSubmissionFn:           func(s string) error { return startNextSubmission(submissions, s) },
		SendSubmissionQueuedNoticeFn:    func(c context.Context, s *domainsubmission.Submission) { queuedNotice.sendSubmissionQueuedNotice(c, s) },
		MarkSubmissionQueuedReactionsFn: func(s *domainsubmission.Submission) { pendingQueue.MarkSubmissionQueuedReactions(s) },
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
	config     *config.Config
	configView frontendConfigView
	session    func(string) *conversation.Session
}

func (a reviewWorkspaceProviderAdapter) ReviewWorkspaceForSessionKey(sessionKey string) *config.Workspace {
	sess := a.session(sessionKey)
	workspaceID := a.configView.defaultWorkspaceID()
	if sess != nil {
		if wid := sess.WorkspaceID; wid != "" {
			workspaceID = wid
		}
	}
	return config.FindWorkspace(a.config, workspaceID)
}

func (a reviewWorkspaceProviderAdapter) ReviewDefaultWorkspaceID() string {
	return a.configView.defaultWorkspaceID()
}

func (a reviewWorkspaceProviderAdapter) ReviewFindWorkspace(workspaceID string) *config.Workspace {
	return config.FindWorkspace(a.config, workspaceID)
}

type reviewGitProviderAdapter struct{ context func() context.Context }

func (a reviewGitProviderAdapter) ReviewResolveTarget(cwd string, target appreview.TargetSpec) (appreview.TargetSpec, error) {
	return (appreview.GitService{Context: a.context()}).ResolveTarget(cwd, target)
}

func (a reviewGitProviderAdapter) ReviewListBranches(cwd string) ([]appreview.BranchOption, error) {
	return (appreview.GitService{Context: a.context()}).ListBranches(cwd)
}

func (a reviewGitProviderAdapter) ReviewListCommits(cwd string, limit int) ([]appreview.CommitOption, error) {
	return (appreview.GitService{Context: a.context()}).ListCommits(cwd, limit)
}

type reviewTargetResolver struct{ context func() context.Context }

func (r reviewTargetResolver) Resolve(cwd string, target appreview.TargetSpec) (appreview.TargetSpec, error) {
	return (appreview.GitService{Context: r.context()}).ResolveTarget(cwd, target)
}

type reviewDispatcher struct {
	submissions  *appsubmission.SubmissionQueueService
	pendingQueue *appsubmission.PendingQueueService
	queuedNotice OutboundCardService
}

func (d reviewDispatcher) StartNext(key string) error {
	return d.submissions.StartNextSubmission(key)
}
func (d reviewDispatcher) MarkQueued(sub *domainsubmission.Submission) {
	d.pendingQueue.MarkSubmissionQueuedReactions(sub)
}
func (d reviewDispatcher) Notify(ctx context.Context, sub *domainsubmission.Submission) {
	d.queuedNotice.sendSubmissionQueuedNotice(ctx, sub)
}

type ReviewPortInputs struct {
	Runtime      BackendRuntimeDeps
	Forms        *interaction.FormService
	Delivery     *interaction.DeliveryService
	Context      func() context.Context
	Repository   reviewapp.Repository
	Submissions  *appsubmission.SubmissionQueueService
	PendingQueue *appsubmission.PendingQueueService
	Cards        OutboundCardService
}

func ReviewPorts(inputs ReviewPortInputs) reviewapp.Dependencies {
	runtimeDeps := inputs.Runtime
	return reviewapp.Dependencies{
		Forms: inputs.Forms, Delivery: inputs.Delivery, Options: reviewOptions{context: inputs.Context},
		Gateway: func() (reviewapp.Gateway, error) {
			return runtimeDeps.currentBackend().runtime.requireCodexGateway()
		},
		Repository: inputs.Repository, Resolver: reviewTargetResolver{context: inputs.Context},
		Dispatcher: reviewDispatcher{submissions: inputs.Submissions, pendingQueue: inputs.PendingQueue, queuedNotice: inputs.Cards},
	}
}

type reviewOptions struct{ context func() context.Context }

func (o reviewOptions) Branches(cwd string) ([]reviewapp.BranchOption, error) {
	return (appreview.GitService{Context: o.context()}).ListBranches(cwd)
}
func (o reviewOptions) Commits(cwd string, limit int) ([]reviewapp.CommitOption, error) {
	return (appreview.GitService{Context: o.context()}).ListCommits(cwd, limit)
}
