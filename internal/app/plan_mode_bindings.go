package app

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/app/planmode"
	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type codexPlanModeExitPayload = planmode.ExitPayload

const (
	planCommandUsage = planmode.CommandUsage

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

func renderPlanModeStatusText(mode *state.SessionCollaborationMode) string {
	return planmode.RenderPlanModeStatusText(mode)
}

func resolvePlanModeForActiveThread(a *App) (*state.SessionCollaborationMode, error) {
	return planmode.ResolvePlanModeForActiveThread(newPlanModeAppAdapter(a))
}

func planModeForSession(a *App, sessionKey string) *state.SessionCollaborationMode {
	return planmode.PlanModeForSession(newPlanModeAppAdapter(a), sessionKey)
}

func resolveDefaultCodexCollaborationModeForSession(a *App, sess *state.Session) (*state.SessionCollaborationMode, error) {
	return planmode.ResolveDefaultCodexCollaborationModeForSession(newPlanModeAppAdapter(a), sess)
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

func contentCardTitleForSubmission(a *App, sub *state.Submission, title string) string {
	return planmode.ContentCardTitleForSubmission(newPlanModeAppAdapter(a), sub, title)
}

func contentCardTitleForSession(a *App, sessionKey, workspaceID, title string) string {
	return planmode.ContentCardTitleForSession(newPlanModeAppAdapter(a), sessionKey, workspaceID, title)
}

func codexCollaborationModeForTurnStart(a *App, sessionKey, threadID string) *codexrpc.CollaborationMode {
	return planmode.CodexCollaborationModeForTurnStart(newPlanModeAppAdapter(a), sessionKey, threadID)
}

func codexCollaborationModeFromState(mode *state.SessionCollaborationMode) *codexrpc.CollaborationMode {
	return planmode.CodexCollaborationModeFromState(mode)
}

func defaultCollaborationModeWithConfiguredEffort(a *App, mode *state.SessionCollaborationMode) *state.SessionCollaborationMode {
	return planmode.DefaultCollaborationModeWithConfiguredEffort(newPlanModeAppAdapter(a), mode)
}

func normalizeThreadCollaborationMode(mode *state.SessionCollaborationMode) *state.SessionCollaborationMode {
	return planmode.NormalizeThreadCollaborationMode(mode)
}

func codexPlanModeExitPayloadFromPending(pending *state.PendingRequest) codexPlanModeExitPayload {
	return planmode.ExitPayloadFromPending(pending)
}

func codexPlanModeExitPromptButtons(requestID string) []feishu.Button {
	return planmode.ExitPromptButtons(requestID)
}

func codexPlanModeExitPromptCard(a *App, sessionKey, workspaceID, planMarkdown, requestID string) map[string]any {
	return planmode.ExitPromptCard(newPlanModeAppAdapter(a), sessionKey, workspaceID, planMarkdown, requestID)
}

func codexPlanModeExitSuccessCard(a *App, sessionKey, workspaceID, title, body string) map[string]any {
	return planmode.ExitSuccessCard(newPlanModeAppAdapter(a), sessionKey, workspaceID, title, body)
}

func codexPlanModeExitFailureCard(a *App, sessionKey, workspaceID, body string) map[string]any {
	return planmode.ExitFailureCard(newPlanModeAppAdapter(a), sessionKey, workspaceID, body)
}

func codexPlanModeExitExpiredCard(a *App, sessionKey, workspaceID, body string) map[string]any {
	return planmode.ExitExpiredCard(newPlanModeAppAdapter(a), sessionKey, workspaceID, body)
}

func codexPlanModeExitPendingRequest(a *App, sessionKey string) *state.PendingRequest {
	return planmode.ExitPendingRequest(newPlanModeAppAdapter(a), sessionKey)
}

func codexPlanModeExitOtherOpenPendingExists(a *App, sessionKey, excludeID string) bool {
	return planmode.ExitOtherOpenPendingExists(newPlanModeAppAdapter(a), sessionKey, excludeID)
}

func codexPlanModeExitSessionHasPlanExitBlockers(sess *state.Session) bool {
	return planmode.SessionHasPlanExitBlockers(newPlanModeAppAdapter(nil), sess)
}

func invalidateCodexPlanModeExitArtifactsForSession(a *App, sessionKey, reason string) {
	planmode.InvalidateCodexPlanModeExitArtifactsForSession(newPlanModeAppAdapter(a), sessionKey, reason)
}

func processCodexPlanModeExitOnTurnCompleted(a *App, sessionKey string, sub *state.Submission, threadID, turnID, status string, flush turnStreamFlushResult) bool {
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
		if replyErr := a.feishu.ReplyText(context.Background(), messageID, text, planActionReplyInThread(a, sessionKey)); replyErr != nil {
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

func codexPlanModeExitFreshPrompt(planMarkdown string) string {
	return planmode.FreshPrompt(planMarkdown)
}

func codexPlanModeExitPlanMarkdownFromPending(pending *state.PendingRequest) string {
	return planmode.PlanMarkdownFromPending(pending)
}

func clearCodexPlanModeForSession(a *App, sessionKey string) (bool, error) {
	return planmode.ClearCodexPlanModeForSession(newPlanModeAppAdapter(a), sessionKey)
}

type planModeAppAdapter struct {
	*App
}

func newPlanModeAppAdapter(a *App) planModeAppAdapter {
	return planModeAppAdapter{App: a}
}

func (a planModeAppAdapter) State() planmode.StateProvider {
	if a.App == nil {
		return nil
	}
	return a.App.State()
}

func (a planModeAppAdapter) Feishu() FeishuClient {
	if a.App == nil {
		return nil
	}
	return a.App.feishu
}

func (a planModeAppAdapter) CodexClient() (planmode.CodexClient, error) {
	return requireCodexClient(a.App)
}

func (a planModeAppAdapter) MakeSessionKey(msg *feishu.InboundMessage) string {
	return makeSessionKey(a.App, msg)
}

func (a planModeAppAdapter) ReplyInThreadEnabled(chatType string) bool {
	return replyInThreadEnabled(a.App, chatType)
}

func (a planModeAppAdapter) SessionHasActiveWork(sess *state.Session) bool {
	return sessionHasActiveWork(sess)
}

func (a planModeAppAdapter) ActionStringValue(action *feishu.CardAction, key string) string {
	return actionStringValue(action, key)
}

func (a planModeAppAdapter) RunAsync(fn func()) {
	runAsync(a.App, fn)
}

func (a planModeAppAdapter) ReplyInThreadForSubmission(sub *state.Submission) bool {
	return replyInThreadForSubmission(a.App, sub)
}

func (a planModeAppAdapter) SendLocalTurnFollowupCard(ctx context.Context, parentMessageID string, card map[string]any, replyInThread bool, sub *state.Submission, kind string) (string, error) {
	return sendLocalTurnFollowupCard(ctx, a.App, parentMessageID, card, replyInThread, sub, kind)
}

func (a planModeAppAdapter) StartNextSubmission(sessionKey string) error {
	return startNextSubmission(a.App, sessionKey)
}

func (a planModeAppAdapter) StartWorkspaceThread(sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error) {
	return conversationBackend(a.App).StartWorkspaceThread(sessionKey, sess, ws)
}
