package app

import (
	"feidex/internal/domain/conversation"

	"context"

	appbackend "feidex/internal/app/backend"

	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newThreadMenuDependencies(a *App) appthreadmenu.Dependencies {
	if a == nil {
		return appthreadmenu.Dependencies{}
	}
	return appthreadmenu.Dependencies{
		ConfigProvider: a, FeishuClient: a.feishu,
		AppStateFn: a.ThreadMenuAppState, EffectiveSessionKeyFn: a.ThreadMenuEffectiveSessionKey,
		ConversationBackendFn: a.ThreadMenuConversationBackend, BackendRuntimeFn: a.ThreadMenuBackendRuntime,
		PendingQueueFn: a.ThreadMenuPendingQueue, WorkspaceThreadFn: a.ThreadMenuWorkspaceThread,
		WorkspaceConfigFn: a.ThreadMenuWorkspaceConfig, BackendActionsFn: a.ThreadMenuBackendActions,
		BackendDriver:          a.BackendDriver(),
		SessionHasActiveWorkFn: sessionHasActiveWork,
		CancelAutoRetryFn:      a.CancelAutoRetry, LockAutoRetryDispatchFn: a.LockAutoRetryDispatch,
		ReplyCommandActionResponseFn: a.ReplyCommandActionResponse, CommandForkFn: a.CommandFork,
		CompleteMenuCommandFn: a.CompleteMenuCommand, ActionStringValueFn: actionStringValue,
		MenuCardBodyFn: menuCardBody, MenuCardBodyForBackendFn: menuCardBodyForBackend,
		NormalizeRequestedClaudePermissionModeFn:  a.NormalizeRequestedClaudePermissionMode,
		ApplyClaudePermissionModeToRuntimeFn:      a.ApplyClaudePermissionModeToRuntime,
		ApplyClaudePermissionModeToRuntimeAsyncFn: a.ApplyClaudePermissionModeToRuntimeAsync,
		RenderClaudeSessionPermissionMenuCardFn:   a.RenderClaudeSessionPermissionMenuCard,
		ShowClaudeSessionPermissionMenuFromAppFn:  a.ShowClaudeSessionPermissionMenuFromApp,
	}
}

func threadMenuService(a *App) *appthreadmenu.Service {
	if a == nil {
		return appthreadmenu.NewService(appthreadmenu.Dependencies{})
	}
	if a.composition == nil {
		a.composition = &appComposition{}
	}
	a.composition.mu.Lock()
	defer a.composition.mu.Unlock()
	if a.composition.threadMenu == nil {
		a.composition.threadMenu = appthreadmenu.NewService(newThreadMenuDependencies(a))
	}
	return a.composition.threadMenu
}

// ---------------------------------------------------------------------------
// Provider adapters — satisfy threadmenu narrow interfaces
// ---------------------------------------------------------------------------

type threadMenuConversationBackendAdapter struct {
	app *App
}

func (a threadMenuConversationBackendAdapter) RenderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	return renderThreadsCard(a.app, sessionKey, includeAll)
}
func (a threadMenuConversationBackendAdapter) InterruptActiveTurn(ctx context.Context, sessionKey string, sess *conversation.Session) error {
	return interruptConversation(a.app, ctx, sessionKey, sess)
}
func (a threadMenuConversationBackendAdapter) ContinueActiveTurn(sessionKey string, text string) error {
	return newConversationService(a.app).ContinueActiveTurn(sessionKey, text)
}
func (a threadMenuConversationBackendAdapter) ResumeSelectedThread(sessionKey string, sess *conversation.Session, ws *config.Workspace, selection appthreadmenu.ThreadResumeSelection) (*appthreadmenu.ThreadBinding, error) {
	return newConversationService(a.app).ResumeSelectedThread(sessionKey, sess, ws, conversation.ThreadSelection(selection))
}
func (a threadMenuConversationBackendAdapter) ForkReplyMessage(forkedID string) string {
	return forkReplyMessage(a.app, forkedID)
}

type threadMenuBackendRuntimeAdapter struct {
	app     *App
	runtime backendRuntimeFacade
}

func (a threadMenuBackendRuntimeAdapter) ReconcileCompletedTurnFromFinalOutput(sessionKey string, sess *conversation.Session) *conversation.Session {
	if a.runtime == nil {
		return sess
	}
	return a.runtime.reconcileCompletedTurnFromFinalOutput(a.app, sessionKey, sess)
}

func (a threadMenuBackendRuntimeAdapter) ClearActiveOperationsAfterInterrupt(sessionKey string, sess *conversation.Session) *conversation.Session {
	if a.runtime == nil {
		return sess
	}
	return a.runtime.clearActiveOperationsAfterInterrupt(a.app, sessionKey, sess)
}

type threadMenuBackendActionAdapter struct {
	service appbackend.ActionService
}

func (a threadMenuBackendActionAdapter) CompleteMenuInterrupt(action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	return a.service.CompleteMenuInterrupt(action, sessionKey, targetTurnID)
}

// ---------------------------------------------------------------------------
// *App methods satisfying threadmenu.App
// ---------------------------------------------------------------------------

func (a *App) ThreadMenuAppState() appthreadmenu.AppStateProvider {
	if a == nil {
		return nil
	}
	return a.State()
}

func (a *App) ThreadMenuEffectiveSessionKey(sessionKey string) string {
	return threadMenuEffectiveSessionKey(a, sessionKey)
}

func (a *App) ThreadMenuConversationBackend() appthreadmenu.ConversationBackendProvider {
	return threadMenuConversationBackendAdapter{app: a}
}

func (a *App) ThreadMenuBackendRuntime() appthreadmenu.BackendRuntimeProvider {
	return threadMenuBackendRuntimeAdapter{app: a, runtime: backendRuntime(a)}
}

func (a *App) ThreadMenuPendingQueue() appthreadmenu.PendingQueueProvider {
	return newPendingQueueService(a)
}

func (a *App) ThreadMenuWorkspaceThread() appthreadmenu.WorkspaceThreadProvider {
	return newConversationService(a)
}

func (a *App) ThreadMenuWorkspaceConfig() appthreadmenu.WorkspaceConfigProvider {
	return threadMenuWorkspaceConfigAdapter{app: a}
}

func (a *App) ThreadMenuBackendActions() appthreadmenu.BackendActionProvider {
	return threadMenuBackendActionAdapter{service: newBackendActionService(a)}
}

func (a *App) CommandFork(msg *feishu.InboundMessage, args []string) error {
	return commandFork(a, msg, args)
}

func (a *App) CompleteMenuCommand(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(a, action, sessionKey, rawCommand, parentAction)
}

func (a *App) ActionStringValue(action *feishu.CardAction, key string) string {
	return actionStringValue(action, key)
}

func (a *App) MenuCardBodyForBackend(backend, action, body string) string {
	return menuCardBodyForBackend(backend, action, body)
}

func (a *App) CancelAutoRetry(sessionKey string, keepUntilTerminal bool, notice string) bool {
	return newAutoRetryService(a).CancelAutoRetry(sessionKey, keepUntilTerminal, notice)
}

func (a *App) NormalizeRequestedClaudePermissionMode(ctx context.Context, raw string) (string, string, error) {
	return normalizeRequestedClaudePermissionMode(a, ctx, raw)
}

func (a *App) ApplyClaudePermissionModeToRuntime(sessionKey, mode string) error {
	return applyClaudePermissionModeToRuntime(a, sessionKey, mode)
}

// ApplyClaudePermissionModeToRuntimeAsync enqueues the runtime apply so a card
// callback can ack immediately; a failure patches the menu card.
func (a *App) ApplyClaudePermissionModeToRuntimeAsync(messageID, sessionKey, mode string) {
	applyClaudePermissionModeToRuntimeAsync(a, messageID, sessionKey, mode)
}

func (a *App) RenderClaudeSessionPermissionMenuCard(sessionKey string) (map[string]any, error) {
	return renderClaudeSessionPermissionMenuCard(a, sessionKey)
}

func (a *App) ShowClaudeSessionPermissionMenuFromApp(msg *feishu.InboundMessage) error {
	return showClaudeSessionPermissionMenu(a, msg)
}

type threadMenuWorkspaceConfigAdapter struct {
	app *App
}

func (a threadMenuWorkspaceConfigAdapter) CurrentThreadForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace, threadID string, err error) {
	return currentThreadForMessage(a.app, msg)
}

func (a *App) LockAutoRetryDispatch(sessionKey string) func() {
	return a.AutoRetries().LockDispatch(sessionKey)
}
