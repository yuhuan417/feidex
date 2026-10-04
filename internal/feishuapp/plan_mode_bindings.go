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
	"log/slog"
	"strings"
	"sync"

	"feidex/internal/config"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
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

func commandPlan(a *App, msg *feishu.InboundMessage, args []string) error {
	return planmode.CommandPlan(newPlanModeAppAdapter(a), msg, args)
}

func (a *App) PlanModeTitleForSession(sessionKey, title string) string {
	return planModeTitleForSession(a, sessionKey, title)
}

func planModeTitleForSession(a *App, sessionKey, title string) string {
	return planmode.PlanModeTitleForSession(planPresentationContext(a), sessionKey, title)
}

func (a *App) ContentCardTitleForSession(sessionKey, workspaceID, title string) string {
	return contentCardTitleForSession(a, sessionKey, workspaceID, title)
}

func contentCardTitleForSubmission(state planmode.SessionStateProvider, sub *domainsubmission.Submission, title string) string {
	return planmode.ContentCardTitleForSubmissionFromState(state, true, sub, title)
}

func contentCardTitleForSession(a *App, sessionKey, workspaceID, title string) string {
	return planmode.ContentCardTitleForSession(planPresentationContext(a), sessionKey, workspaceID, title)
}

func planPresentationContext(a *App) planmode.Dependencies {
	if a == nil {
		return planmode.Dependencies{}
	}
	return planmode.Dependencies{ConfigProvider: a, StateProvider: a.State()}
}

func planModeStateForTurnStart(plan *planapp.Service, sessionKey, threadID string) *conversation.SessionCollaborationMode {
	return plan.ModeForTurnStart(sessionKey, threadID)
}

func normalizeThreadCollaborationMode(mode *conversation.SessionCollaborationMode) *conversation.SessionCollaborationMode {
	return conversation.NormalizeCollaborationMode(mode)
}

func codexPlanModeExitPendingRequest(a *App, sessionKey string) *state.PendingRequest {
	return planmode.ExitPendingRequest(newPlanModeAppAdapter(a), sessionKey)
}

func processCodexPlanModeExitOnTurnCompleted(a *App, sessionKey string, sub *domainsubmission.Submission, threadID, turnID, status string, flush turnStreamFlushResult) bool {
	return planmode.ProcessCodexPlanModeExitOnTurnCompleted(newPlanModeAppAdapter(a), sessionKey, sub, threadID, turnID, status, planmode.TurnStreamFlushResult{
		ShouldUsePlanExitPrompt: flush.ShouldUsePlanExitPrompt,
		PlanMarkdown:            flush.PlanMarkdown,
		PlanMessageID:           flush.PlanMessageID,
	})
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

func completeCodexPlanModeExit(a *App, action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	return planmode.CompleteCodexPlanModeExit(newPlanModeAppAdapter(a), action, actionName)
}

func completeMenuPlanAsync(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return completeMenuCommand(a, action, sessionKey, "/plan", "menu.tools")
	}
	messageID := strings.TrimSpace(action.MessageID)
	runAsync(a, func() {
		resp, err := completeMenuCommand(a, action, sessionKey, "/plan", "menu.tools")
		if card := callbackResponseCard(resp); card != nil {
			patchMaintenanceCard(a.Context(), a.FrontendID(), newEffectRunner(a.runtimeOwner), messageID, card, "plan menu patch failed",
				"session_key", sessionKey,
				"message_id", messageID,
			)
			return
		}
		text := callbackResponseToastText(resp)
		if err != nil {
			text = err.Error()
		}
		text = strings.TrimSpace(text)
		if text == "" || a == nil || a.feishu == nil {
			return
		}
		if replyErr := newEffectOutbound(a.FrontendID(), newEffectRunner(a.runtimeOwner)).ReplyText(context.Background(), messageID, text, actionReplyInThreadForSession(a.State().Session, sessionKey, a.configView().replyInThreadEnabled())); replyErr != nil {
			slog.Warn("plan async text reply failed",
				"session_key", sessionKey,
				"message_id", messageID,
				"error", replyErr,
			)
		}
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在处理 plan mode"},
	}, nil
}

func clearCodexPlanModeForSession(a *App, sessionKey string) (bool, error) {
	return planmode.ClearCodexPlanModeForSession(newPlanModeAppAdapter(a), sessionKey)
}

func newPlanModeAppAdapter(a *App) planmode.Dependencies {
	if a == nil {
		return planmode.Dependencies{}
	}
	return PlanModePorts(PlanModePortInputs{
		Runtime: a.BackendRuntimeDeps(), UseCase: a.bindings.Plan, Continuation: a.bindings.Continuation,
		State: a.State(), ModelSnapshots: a.bindings.ModelSnapshots,
		WorkspaceSelection: a.bindings.WorkspaceSelection, Submissions: a.bindings.Submissions,
		Conversations: a.bindings.Conversations, Feishu: a.feishu, AsyncRunner: a.asyncRunner,
	})
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
