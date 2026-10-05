package feishuapp

import (
	"feidex/internal/domain/conversation"
	backendruntime "feidex/internal/runtime"

	"context"
	"strings"

	appbackend "feidex/internal/adapter/feishu/backend"

	appthreadmenu "feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/adapter/feishu/workspacecmd"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newThreadMenuDependencies(a *App) appthreadmenu.Dependencies {
	if a == nil {
		return appthreadmenu.Dependencies{}
	}
	permissionBackend := ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex())
	permissionMenuRenderer := ClaudePermissionMenuRenderer(a.Config(), permissionBackend, a.State().Session)
	autoRetryTracker := a.runtimeOwner.AutoRetries
	return appthreadmenu.Dependencies{
		ConfigProvider: a, Outbound: newEffectOutbound(a.FrontendID(), newEffectRunner(a.runtimeOwner)), Controls: a.bindings.ConversationControls, Settings: a.bindings.ThreadSettings,
		PermissionSettings: a.bindings.PermissionSettings,
		AppStateFn:         a.ThreadMenuAppState, EffectiveSessionKeyFn: a.ThreadMenuEffectiveSessionKey,
		ConversationBackendFn: a.ThreadMenuConversationBackend, BackendRuntimeFn: a.ThreadMenuBackendRuntime,
		PendingQueueFn: a.ThreadMenuPendingQueue, WorkspaceThreadFn: a.ThreadMenuWorkspaceThread,
		WorkspaceConfigFn: a.ThreadMenuWorkspaceConfig, BackendActionsFn: a.ThreadMenuBackendActions,
		BackendDriver:          a.BackendDriver(),
		SessionHasActiveWorkFn: sessionHasActiveWork,
		CancelAutoRetryFn:      a.CancelAutoRetry, LockAutoRetryDispatchFn: autoRetryTracker.LockDispatch,
		ReplyCommandActionResponseFn: a.ReplyCommandActionResponse, CommandForkFn: a.CommandFork,
		CompleteMenuCommandFn: a.CompleteMenuCommand, ActionStringValueFn: actionStringValue,
		MenuCardBodyFn: menuCardBody, MenuCardBodyForBackendFn: menuCardBodyForBackend,
		RenderClaudeSessionPermissionMenuCardFn:  permissionMenuRenderer,
		ShowClaudeSessionPermissionMenuFromAppFn: a.ShowClaudeSessionPermissionMenuFromApp,
	}
}

// ---------------------------------------------------------------------------
// Provider adapters — satisfy threadmenu narrow interfaces
// ---------------------------------------------------------------------------

type threadMenuConversationBackendAdapter struct {
	threadCards   threadCardInputs
	conversations *conversationapp.Service
	runtimeDeps   BackendRuntimeDeps
}

func (a threadMenuConversationBackendAdapter) RenderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	return renderThreadsCard(a.threadCards, sessionKey, includeAll)
}
func (a threadMenuConversationBackendAdapter) InterruptActiveTurn(ctx context.Context, sessionKey string, sess *conversation.Session) error {
	return interruptConversation(a.conversations, a.runtimeDeps, ctx, sessionKey, sess)
}
func (a threadMenuConversationBackendAdapter) ContinueActiveTurn(sessionKey string, text string) error {
	return a.conversations.ContinueActiveTurn(sessionKey, text)
}
func (a threadMenuConversationBackendAdapter) ResumeSelectedThread(sessionKey string, sess *conversation.Session, ws *config.Workspace, selection appthreadmenu.ThreadResumeSelection) (*appthreadmenu.ThreadBinding, error) {
	return a.conversations.ResumeSelectedThread(sessionKey, sess, ws, conversation.ThreadSelection(selection))
}
func (a threadMenuConversationBackendAdapter) ForkReplyMessage(forkedID string) string {
	return forkReplyMessage(a.threadCards.Backend(), forkedID)
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
	if a == nil {
		return strings.TrimSpace(sessionKey)
	}
	return threadMenuEffectiveSessionKey(a.configView().normalizeSessionKey, a.bindings.BindingCommands.scope, a.bindings.ConversationQuery, sessionKey)
}

func (a *App) ThreadMenuConversationBackend() appthreadmenu.ConversationBackendProvider {
	backend := ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex())
	conversations := a.bindings.Conversations
	return threadMenuConversationBackendAdapter{
		threadCards:   threadCardInputs{Repository: a.State(), Config: a.Config(), Backend: backend, Conversations: conversations},
		conversations: conversations, runtimeDeps: a.BackendRuntimeDeps(),
	}
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
	return threadMenuWorkspaceConfigAdapter{
		workspaceConfiguration: a.bindings.WorkspaceConfiguration,
		backend:                ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex()),
	}
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

func (a *App) ShowClaudeSessionPermissionMenuFromApp(msg *feishu.InboundMessage) error {
	return showClaudeSessionPermissionMenu(a, msg)
}

type threadMenuWorkspaceConfigAdapter struct {
	workspaceConfiguration *workspacecmd.ConfigService
	backend                func() string
}

func (a threadMenuWorkspaceConfigAdapter) CurrentThreadForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace, threadID string, err error) {
	return currentThreadForMessage(a.workspaceConfiguration, a.backend, msg)
}
