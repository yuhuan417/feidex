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
	appstate "feidex/internal/adapter/storage/json/scoped"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/config"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
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

type ReviewCommandInputs struct {
	Runtime            BackendRuntimeDeps
	Store              *state.Store
	WorkspaceSelection workspaceapp.SelectionService
	UseCase            *reviewapp.Service
	Submissions        *appsubmission.SubmissionQueueService
	BindingScope       BindingScope
	PendingQueue       *appsubmission.PendingQueueService
	QueuedNotice       OutboundCardService
	Feishu             FeishuClient
	State              *appstate.Store
	Effects            frontendruntime.EffectRunner
	AsyncActions       AsyncCardActionService
}

func BuildReviewCommandDependencies(inputs ReviewCommandInputs) appreviewcmd.Dependencies {
	runtimeDeps := inputs.Runtime
	configProvider := newFrontendConfigProvider(runtimeDeps, inputs.Store, inputs.WorkspaceSelection)
	bindingScope := inputs.BindingScope.scope
	return appreviewcmd.Dependencies{
		UseCase:        inputs.UseCase,
		ConfigProvider: configProvider, Outbound: newEffectOutbound(runtimeDeps.frontendID, inputs.Effects), CardRenderer: simpleStatusCardRenderer{client: inputs.Feishu}, StateProvider: inputs.State,
		ContextProvider:        configProvider,
		WorkspaceProviderValue: reviewWorkspaceProviderAdapter{config: runtimeDeps.cfg, defaultWorkspaceID: runtimeDeps.view.defaultWorkspaceID, session: inputs.State.Session}, GitProvider: reviewGitProviderAdapter{context: configProvider.Context},
		CodexClientFn: func() (appreviewcmd.CodexClient, error) {
			return runtimeDeps.currentBackend().runtime.requireCodexGateway()
		},
		MakeSessionKeyFn: runtimeDeps.view.makeSessionKey, ReplyInThreadEnabledFn: func(string) bool { return runtimeDeps.view.replyInThreadEnabled() },
		MenuCardBodyFn: menuCardBody, ActionStringValueFn: actionStringValue,
		CommandMessageFromActionFn: func(x *feishu.CardAction, s, r string) *feishu.InboundMessage {
			return commandMessageFromAction(bindingScope, x, s, r)
		},
		SessionHasActiveWorkFn: sessionHasActiveWork, SessionHasInFlightSubmissionFn: conversation.HasInFlightSubmission,
		StartNextSubmissionFn: func(s string) error { return startNextSubmission(inputs.Submissions, s) },
		SendSubmissionQueuedNoticeFn: func(c context.Context, s *domainsubmission.Submission) {
			inputs.QueuedNotice.sendSubmissionQueuedNotice(c, s)
		},
		MarkSubmissionQueuedReactionsFn: func(s *domainsubmission.Submission) { inputs.PendingQueue.MarkSubmissionQueuedReactions(s) },
		CompleteAsyncCommandActionFn: func(x *feishu.CardAction, s, r, f, t string, p map[string]any, ok, fail func(string, string) map[string]any, w string) (*callback.CardActionTriggerResponse, error) {
			return inputs.AsyncActions.CompleteCommand(x, s, r, f, t, p, ok, fail, w)
		},
		CompleteAsyncRenderedCardActionFn: func(x *feishu.CardAction, s, t string, p map[string]any, r func() (*callback.CardActionTriggerResponse, error), f func(string, string) map[string]any, w string) (*callback.CardActionTriggerResponse, error) {
			return inputs.AsyncActions.CompleteRendered(x, s, t, p, r, f, w)
		},
	}
}

// ---------------------------------------------------------------------------
// Internal adapter types
// ---------------------------------------------------------------------------

// reviewWorkspaceProviderAdapter wraps workspace access for the review
// service.
type reviewWorkspaceProviderAdapter struct {
	config             *config.Config
	defaultWorkspaceID func() string
	session            func(string) *conversation.Session
}

func (a reviewWorkspaceProviderAdapter) ReviewWorkspaceForSessionKey(sessionKey string) *config.Workspace {
	sess := a.session(sessionKey)
	workspaceID := a.defaultWorkspaceID()
	if sess != nil {
		if wid := sess.WorkspaceID; wid != "" {
			workspaceID = wid
		}
	}
	return config.FindWorkspace(a.config, workspaceID)
}

func (a reviewWorkspaceProviderAdapter) ReviewDefaultWorkspaceID() string {
	return a.defaultWorkspaceID()
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
