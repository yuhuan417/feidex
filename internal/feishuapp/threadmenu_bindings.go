package feishuapp

import (
	"feidex/internal/domain/conversation"
	backendruntime "feidex/internal/runtime"

	"context"

	appbackend "feidex/internal/adapter/feishu/backend"

	appthreadmenu "feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newThreadMenuDependencies(a *App) appthreadmenu.Dependencies {
	if a == nil {
		return appthreadmenu.Dependencies{}
	}
	return appthreadmenu.Dependencies{
		ConfigProvider: a, Outbound: newEffectOutbound(a.FrontendID(), newEffectRunner(a.runtimeOwner)), Controls: a.bindings.ConversationControls, Settings: a.bindings.ThreadSettings,
		PermissionSettings: a.bindings.PermissionSettings,
		AppStateFn:         a.ThreadMenuAppState, EffectiveSessionKeyFn: a.ThreadMenuEffectiveSessionKey,
		ConversationBackendFn: a.ThreadMenuConversationBackend, BackendRuntimeFn: a.ThreadMenuBackendRuntime,
		PendingQueueFn: a.ThreadMenuPendingQueue, WorkspaceThreadFn: a.ThreadMenuWorkspaceThread,
		WorkspaceConfigFn: a.ThreadMenuWorkspaceConfig, BackendActionsFn: a.ThreadMenuBackendActions,
		BackendDriver:          a.BackendDriver(),
		SessionHasActiveWorkFn: sessionHasActiveWork,
		CancelAutoRetryFn:      a.CancelAutoRetry, LockAutoRetryDispatchFn: a.LockAutoRetryDispatch,
		ReplyCommandActionResponseFn: a.ReplyCommandActionResponse, CommandForkFn: a.CommandFork,
		CompleteMenuCommandFn: a.CompleteMenuCommand, ActionStringValueFn: actionStringValue,
		MenuCardBodyFn: menuCardBody, MenuCardBodyForBackendFn: menuCardBodyForBackend,
		RenderClaudeSessionPermissionMenuCardFn:  a.RenderClaudeSessionPermissionMenuCard,
		ShowClaudeSessionPermissionMenuFromAppFn: a.ShowClaudeSessionPermissionMenuFromApp,
	}
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
	return interruptConversation(a.app.bindings.Conversations, a.app.BackendRuntimeDeps(), ctx, sessionKey, sess)
}
func (a threadMenuConversationBackendAdapter) ContinueActiveTurn(sessionKey string, text string) error {
	return a.app.bindings.Conversations.ContinueActiveTurn(sessionKey, text)
}
func (a threadMenuConversationBackendAdapter) ResumeSelectedThread(sessionKey string, sess *conversation.Session, ws *config.Workspace, selection appthreadmenu.ThreadResumeSelection) (*appthreadmenu.ThreadBinding, error) {
	return a.app.bindings.Conversations.ResumeSelectedThread(sessionKey, sess, ws, conversation.ThreadSelection(selection))
}
func (a threadMenuConversationBackendAdapter) ForkReplyMessage(forkedID string) string {
	return forkReplyMessage(a.app, forkedID)
}

type threadMenuBackendRuntimeAdapter struct {
	deps    BackendRuntimeDeps
	runtime backendruntime.BackendFacade
}

func (a threadMenuBackendRuntimeAdapter) ReconcileCompletedTurnFromFinalOutput(sessionKey string, sess *conversation.Session) *conversation.Session {
	if a.runtime == nil {
		return sess
	}
	return a.runtime.ReconcileCompletedTurnFromFinalOutput(backendRuntimeContextForApp(a.deps), sessionKey, sess)
}

func (a threadMenuBackendRuntimeAdapter) ClearActiveOperationsAfterInterrupt(sessionKey string, sess *conversation.Session) *conversation.Session {
	if a.runtime == nil {
		return sess
	}
	return a.runtime.ClearActiveOperationsAfterInterruptContext(backendRuntimeContextForApp(a.deps), sessionKey, sess)
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

func (a *App) ThreadMenuAppState() appthreadmenu.StateProvider {
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
	if a == nil {
		return threadMenuBackendRuntimeAdapter{}
	}
	return threadMenuBackendRuntimeAdapter{
		deps: a.BackendRuntimeDeps(), runtime: backendRuntime(a.configView().configuredBackend()),
	}
}

func (a *App) ThreadMenuPendingQueue() appthreadmenu.PendingQueueProvider {
	return a.bindings.PendingQueue
}

func (a *App) ThreadMenuWorkspaceThread() appthreadmenu.WorkspaceThreadProvider {
	return a.bindings.Conversations
}

func (a *App) ThreadMenuWorkspaceConfig() appthreadmenu.WorkspaceConfigProvider {
	return threadMenuWorkspaceConfigAdapter{app: a}
}

func (a *App) ThreadMenuBackendActions() appthreadmenu.BackendActionProvider {
	return threadMenuBackendActionAdapter{service: a.bindings.BackendActions}
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
	return a.bindings.AutoRetry.CancelAutoRetry(sessionKey, keepUntilTerminal, notice)
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
