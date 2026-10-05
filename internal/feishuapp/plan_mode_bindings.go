package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/application/continuation"
	applicationconversation "feidex/internal/application/conversation"
	modelconfigapp "feidex/internal/application/modelconfig"
	planapp "feidex/internal/application/plan"
	"feidex/internal/application/submission"
	"feidex/internal/application/workspace"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"fmt"
	"strings"
	"sync"

	"feidex/internal/config"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"
)

const (
	codexPlanModeExitPendingKind            = planmode.ExitPendingKind
	codexPlanModeExitImplementCurrentAction = planmode.ExitImplementCurrentAction
	codexPlanModeExitImplementFreshAction   = planmode.ExitImplementFreshAction
	codexPlanModeExitStayAction             = planmode.ExitStayAction
	codexPlanModeExitPendingTitle           = planmode.ExitPendingTitle
	codexPlanModeExitExpiredTitle           = planmode.ExitExpiredTitle
	codexPlanModeExitFollowupKind           = planmode.ExitFollowupKind
)

func planModeTitleForSession(state planmode.SessionStateProvider, enabled bool, sessionKey, title string) string {
	return planmode.PlanModeTitleForSessionFromState(state, enabled, sessionKey, title)
}

func contentCardTitleForSubmission(state planmode.SessionStateProvider, sub *domainsubmission.Submission, title string) string {
	return planmode.ContentCardTitleForSubmissionFromState(state, true, sub, title)
}

func contentCardTitleForSession(state planmode.SessionStateProvider, enabled bool, sessionKey, workspaceID, title string) string {
	return planmode.ContentCardTitleForSessionFromState(state, enabled, sessionKey, workspaceID, title)
}

func planModeStateForTurnStart(plan *planapp.Service, sessionKey, threadID string) *conversation.SessionCollaborationMode {
	return plan.ModeForTurnStart(sessionKey, threadID)
}

func normalizeThreadCollaborationMode(mode *conversation.SessionCollaborationMode) *conversation.SessionCollaborationMode {
	return conversation.NormalizeCollaborationMode(mode)
}

type PlanModePortInputs struct {
	Runtime            BackendRuntimeDeps
	UseCase            *planapp.Service
	Continuation       *continuation.Service
	State              planmode.StateProvider
	ModelSnapshots     modelconfigapp.SnapshotService
	WorkspaceSelection workspace.SelectionService
	Submissions        *submission.SubmissionQueueService
	Conversations      *applicationconversation.Service
	Feishu             FeishuClient
	AsyncRunner        func(func())
}

type planModeConfigProvider struct {
	runtime    BackendRuntimeDeps
	store      *state.Store
	workspaces workspace.SelectionService
}

func (p planModeConfigProvider) Config() *config.Config  { return p.runtime.cfg }
func (p planModeConfigProvider) ConfigMu() *sync.RWMutex { return p.runtime.view.mu }
func (p planModeConfigProvider) Backend() string {
	if p.runtime.runtime.owner == nil {
		return p.runtime.view.configuredBackend()
	}
	return p.runtime.runtime.owner.Backend()
}
func (p planModeConfigProvider) FrontendID() string { return p.runtime.frontendID }
func (p planModeConfigProvider) FrontendConfigIndex() int {
	return p.runtime.view.frontendConfigIndex
}
func (p planModeConfigProvider) Store() *state.Store { return p.store }
func (p planModeConfigProvider) WorkspaceSelection() workspace.SelectionService {
	return p.workspaces
}

type planModeLifecycleContext struct {
	owner *frontendruntime.FrontendOwner
}

func (p planModeLifecycleContext) Context() context.Context {
	if p.owner == nil {
		return context.Background()
	}
	return p.owner.Lifecycle.Context()
}

func PlanModePorts(inputs PlanModePortInputs) planmode.Dependencies {
	deps := inputs.Runtime
	owner := deps.runtime.owner
	if owner == nil {
		return planmode.Dependencies{}
	}
	outbound := newEffectOutbound(deps.frontendID, *owner.EffectRunner)
	links := newMessageLinkRecorder(deps.view, owner, inputs.Continuation)
	return planmode.Dependencies{
		UseCase:         inputs.UseCase,
		ConfigProvider:  planModeConfigProvider{runtime: deps, store: deps.store, workspaces: inputs.WorkspaceSelection},
		ContextProvider: planModeLifecycleContext{owner: owner}, StateProvider: inputs.State,
		Outbound: outbound, CardRenderer: simpleStatusCardRenderer{client: inputs.Feishu},
		CodexClientProvider:    func() (planmode.CodexClient, error) { return deps.currentBackend().runtime.requireCodexGateway() },
		MakeSessionKeyFn:       deps.view.makeSessionKey,
		ReplyInThreadEnabledFn: func(string) bool { return deps.view.replyInThreadEnabled() },
		SessionHasActiveWorkFn: sessionHasActiveWork,
		EffectivePlanSettingsFn: func(sess *conversation.Session) (string, string) {
			settings := inputs.ModelSnapshots.Desired(domainbackend.BackendCodex, sess)
			return settings.PlanModel, settings.PlanEffort
		},
		ActionStringValueFn:          actionStringValue,
		RunAsyncFn:                   func(fn func()) { owner.Lifecycle.Run(fn, inputs.AsyncRunner) },
		ReplyInThreadForSubmissionFn: replyInThreadForSubmission,
		SendLocalTurnFollowupCardFn: func(ctx context.Context, parent string, card map[string]any, reply bool, sub *domainsubmission.Submission, kind string) (string, error) {
			if inputs.Feishu == nil {
				return "", fmt.Errorf("follow-up unavailable")
			}
			return sendLocalTurnFollowupCardWith(outbound, links, ctx, parent, card, reply, sub, kind)
		},
		StartNextSubmissionFn: func(key string) error { return startNextSubmission(inputs.Submissions, key) },
		StartWorkspaceThreadFn: func(key string, sess *conversation.Session, ws *config.Workspace) (*conversation.ThreadBinding, error) {
			return inputs.Conversations.StartWorkspaceThread(key, sess, ws)
		},
	}
}

func sendLocalTurnFollowupCardWith(outbound effectOutbound, links messageLinkRecorder, ctx context.Context, parentMessageID string, card map[string]any, replyInThread bool, sub *domainsubmission.Submission, kind string) (string, error) {
	parentMessageID = strings.TrimSpace(parentMessageID)
	if parentMessageID == "" {
		return "", fmt.Errorf("follow-up parent message missing")
	}
	if card == nil {
		return "", fmt.Errorf("follow-up card unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	messageID, err := outbound.ReplyCard(ctx, parentMessageID, card, replyInThread)
	if err != nil {
		return "", err
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return "", fmt.Errorf("follow-up message id missing")
	}
	if sub != nil && strings.TrimSpace(sub.SessionKey) != "" && strings.TrimSpace(sub.ThreadID) != "" && strings.TrimSpace(sub.TurnID) != "" {
		links.Record(messageID, kind, anchorForSubmission(sub), "")
	}
	return messageID, nil
}

type planSettingsSource struct {
	view      frontendConfigView
	snapshots modelconfigapp.SnapshotService
}

func (s planSettingsSource) Values(sess *conversation.Session) planapp.SettingsValues {
	settings := s.snapshots.Desired(domainbackend.BackendCodex, sess)
	if s.view.mu != nil {
		s.view.mu.RLock()
		defer s.view.mu.RUnlock()
	}
	return planapp.SettingsValues{Experimental: s.view.cfg != nil && s.view.cfg.Codex.ExperimentalAPI, Model: settings.Model, Effort: settings.Effort, PlanModel: settings.PlanModel, PlanEffort: settings.PlanEffort}
}

type planWorkspaces struct{ view frontendConfigView }

func (w planWorkspaces) Get(id string) *config.Workspace { return config.FindWorkspace(w.view.cfg, id) }
func (w planWorkspaces) DefaultID() string               { return w.view.defaultWorkspaceID() }

// PlanPorts captures the configuration and runtime owners, while settings and
// the active backend client remain live reads through those owners.
func PlanPorts(cfg *config.Config, mu *sync.RWMutex, snapshots modelconfigapp.SnapshotService, owner *frontendruntime.FrontendOwner) (planapp.SettingsSource, func() (planapp.Catalog, error), planapp.Workspaces) {
	view := frontendConfigView{cfg: cfg, mu: mu}
	runtime := runtimeView{owner: owner}
	return planSettingsSource{view: view, snapshots: snapshots}, func() (planapp.Catalog, error) { return runtime.requireCodexGateway() }, planWorkspaces{view: view}
}
