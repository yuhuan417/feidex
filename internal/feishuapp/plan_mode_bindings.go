package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/planmode"
	modelconfigapp "feidex/internal/application/modelconfig"
	planapp "feidex/internal/application/plan"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
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
			patchMaintenanceCard(a, messageID, card, "plan menu patch failed",
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
	modelSnapshots := a.bindings.ModelSnapshots
	submissions := a.bindings.Submissions
	conversations := a.bindings.Conversations
	return planmode.Dependencies{
		UseCase:                a.bindings.Plan,
		ConfigProvider:         a,
		ContextProvider:        a,
		StateProvider:          a.State(),
		Outbound:               newEffectOutbound(a.FrontendID(), newEffectRunner(a.runtimeOwner)),
		CardRenderer:           simpleStatusCardRenderer{client: a.feishu},
		CodexClientProvider:    func() (planmode.CodexClient, error) { return a.runtimeView().requireCodexGateway() },
		MakeSessionKeyFn:       func(msg *feishu.InboundMessage) string { return a.configView().makeSessionKey(msg) },
		ReplyInThreadEnabledFn: func(chatType string) bool { return a.configView().replyInThreadEnabled() },
		SessionHasActiveWorkFn: sessionHasActiveWork,
		EffectivePlanSettingsFn: func(sess *conversation.Session) (string, string) {
			settings := modelSnapshots.Desired(domainbackend.BackendCodex, sess)
			return settings.PlanModel, settings.PlanEffort
		},
		ActionStringValueFn:          actionStringValue,
		RunAsyncFn:                   func(fn func()) { runAsync(a, fn) },
		ReplyInThreadForSubmissionFn: func(sub *domainsubmission.Submission) bool { return replyInThreadForSubmission(sub) },
		SendLocalTurnFollowupCardFn: func(ctx context.Context, parent string, card map[string]any, reply bool, sub *domainsubmission.Submission, kind string) (string, error) {
			return sendLocalTurnFollowupCard(ctx, a, parent, card, reply, sub, kind)
		},
		StartNextSubmissionFn: func(key string) error { return startNextSubmission(submissions, key) },
		StartWorkspaceThreadFn: func(key string, sess *conversation.Session, ws *config.Workspace) (*conversation.ThreadBinding, error) {
			return conversations.StartWorkspaceThread(key, sess, ws)
		},
	}
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
