package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/planmode"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"log/slog"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
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
	return planmode.PlanModeTitleForSession(newPlanModeAppAdapter(a), sessionKey, title)
}

func (a *App) ContentCardTitleForSession(sessionKey, workspaceID, title string) string {
	return contentCardTitleForSession(a, sessionKey, workspaceID, title)
}

func contentCardTitleForSubmission(a *App, sub *domainsubmission.Submission, title string) string {
	return planmode.ContentCardTitleForSubmission(newPlanModeAppAdapter(a), sub, title)
}

func contentCardTitleForSession(a *App, sessionKey, workspaceID, title string) string {
	return planmode.ContentCardTitleForSession(newPlanModeAppAdapter(a), sessionKey, workspaceID, title)
}

func planModeStateForTurnStart(a *App, sessionKey, threadID string) *conversation.SessionCollaborationMode {
	return planmode.StateForTurnStart(newPlanModeAppAdapter(a), sessionKey, threadID)
}

func normalizeThreadCollaborationMode(mode *conversation.SessionCollaborationMode) *conversation.SessionCollaborationMode {
	return planmode.NormalizeThreadCollaborationMode(mode)
}

func codexPlanModeExitPendingRequest(a *App, sessionKey string) *state.PendingRequest {
	return planmode.ExitPendingRequest(newPlanModeAppAdapter(a), sessionKey)
}

func invalidateCodexPlanModeExitArtifactsForSession(a *App, sessionKey, reason string) {
	planmode.InvalidateCodexPlanModeExitArtifactsForSession(newPlanModeAppAdapter(a), sessionKey, reason)
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
		if replyErr := replyTextByAnchorEffect(context.Background(), a, messageID, text, planActionReplyInThread(a, sessionKey)); replyErr != nil {
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

func planActionReplyInThread(a *App, sessionKey string) bool {
	if a == nil || strings.TrimSpace(sessionKey) == "" {
		return false
	}
	if sess := a.State().Session(sessionKey); sess != nil {
		return replyInThreadEnabled(a, sess.ChatType)
	}
	return false
}

func clearCodexPlanModeForSession(a *App, sessionKey string) (bool, error) {
	return planmode.ClearCodexPlanModeForSession(newPlanModeAppAdapter(a), sessionKey)
}

func newPlanModeAppAdapter(a *App) planmode.Dependencies {
	if a == nil {
		return planmode.Dependencies{}
	}
	return planmode.Dependencies{
		ConfigProvider:         a,
		ContextProvider:        a,
		StateProvider:          a.State(),
		Outbound:               planModeOutbound{app: a},
		CardRenderer:           planModeCardRenderer{app: a},
		CodexClientProvider:    func() (planmode.CodexClient, error) { return requireCodexGateway(a) },
		MakeSessionKeyFn:       func(msg *feishu.InboundMessage) string { return makeSessionKey(a, msg) },
		ReplyInThreadEnabledFn: func(chatType string) bool { return replyInThreadEnabled(a, chatType) },
		SessionHasActiveWorkFn: sessionHasActiveWork,
		EffectivePlanSettingsFn: func(sess *conversation.Session) (string, string) {
			settings := newModelSnapshotService(a).Desired(domainbackend.BackendCodex, sess)
			return settings.PlanModel, settings.PlanEffort
		},
		ActionStringValueFn:          actionStringValue,
		RunAsyncFn:                   func(fn func()) { runAsync(a, fn) },
		ReplyInThreadForSubmissionFn: func(sub *domainsubmission.Submission) bool { return replyInThreadForSubmission(a, sub) },
		SendLocalTurnFollowupCardFn: func(ctx context.Context, parent string, card map[string]any, reply bool, sub *domainsubmission.Submission, kind string) (string, error) {
			return sendLocalTurnFollowupCard(ctx, a, parent, card, reply, sub, kind)
		},
		StartNextSubmissionFn: func(key string) error { return startNextSubmission(a, key) },
		StartWorkspaceThreadFn: func(key string, sess *conversation.Session, ws *config.Workspace) (*conversation.ThreadBinding, error) {
			return newConversationService(a).StartWorkspaceThread(key, sess, ws)
		},
	}
}

type planModeOutbound struct{ app *App }

func (o planModeOutbound) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	return replyTextByAnchorEffect(ctx, o.app, messageID, text, inThread)
}

func (o planModeOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return replyCardWithIDEffect(ctx, o.app, messageID, card, inThread)
}

func (o planModeOutbound) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	return patchCardEffect(ctx, o.app, messageID, card)
}

type planModeCardRenderer struct{ app *App }

func (r planModeCardRenderer) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	if r.app == nil || r.app.feishu == nil {
		return nil
	}
	return r.app.feishu.SimpleStatusCard(title, color, body, buttons)
}
