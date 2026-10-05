package feishuapp

import (
	"feidex/internal/domain/conversation"
	backendruntime "feidex/internal/runtime"

	"context"
	"sync"

	appbackend "feidex/internal/adapter/feishu/backend"

	appthreadmenu "feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/adapter/feishu/workspacecmd"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/application/workspace"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newThreadMenuDependencies(a *App) appthreadmenu.Dependencies {
	if a == nil {
		return appthreadmenu.Dependencies{}
	}
	runtimeDeps := a.BackendRuntimeDeps()
	owner := a.runtimeOwner
	runtimeDeps.contextFn = owner.Lifecycle.Context
	backend := ConfiguredBackendBuilder(runtimeDeps.cfg, runtimeDeps.view.mu, owner.Backend, runtimeDeps.frontendID, runtimeDeps.view.frontendConfigIndex)
	state := a.State()
	forkConfigView := frontendConfigView{
		cfg: runtimeDeps.cfg, mu: runtimeDeps.view.mu,
		frontendID: runtimeDeps.frontendID, frontendConfigIndex: runtimeDeps.view.frontendConfigIndex,
	}
	conversations := a.bindings.Conversations
	bindingScope := a.bindings.BindingCommands.scope
	conversationQuery := a.bindings.ConversationQuery
	pendingQueue := a.bindings.PendingQueue
	workspaceConfiguration := a.bindings.WorkspaceConfiguration
	workspaceSelection := a.bindings.WorkspaceSelection
	conversationControls := a.bindings.ConversationControls
	threadSettings := a.bindings.ThreadSettings
	permissionSettings := a.bindings.PermissionSettings
	backendActions := a.bindings.BackendActions
	autoRetry := a.bindings.AutoRetry
	effectiveSessionKey := func(sessionKey string) string {
		return threadMenuEffectiveSessionKey(runtimeDeps.view.normalizeSessionKey, bindingScope, conversationQuery, sessionKey)
	}
	permissionMenuRenderer := ClaudePermissionMenuRenderer(runtimeDeps.cfg, backend, state.Session)
	autoRetryTracker := owner.AutoRetries
	threadMenuReplyRunner := newEffectRunner(owner)
	threadMenuReplyInThread := forkConfigView.replyInThreadEnabled()
	forkDependencies := threadForkDependencies{
		repository: state, config: runtimeDeps.cfg, effectiveSessionKey: effectiveSessionKey,
		makeSessionKey: forkConfigView.makeSessionKey, backend: backend,
		defaultWorkspaceID: forkConfigView.defaultWorkspaceID, pendingQueue: pendingQueue,
		conversations: conversations, runner: newEffectRunner(owner),
		frontend: identity.FrontendID(runtimeDeps.frontendID), replyInThread: forkConfigView.replyInThreadEnabled(),
	}
	return appthreadmenu.Dependencies{
		ConfigProvider: threadMenuConfigProvider{runtime: runtimeDeps, store: a.store, workspaces: workspaceSelection}, Outbound: newEffectOutbound(runtimeDeps.frontendID, newEffectRunner(owner)), Controls: conversationControls, Settings: threadSettings,
		PermissionSettings: permissionSettings,
		AppStateFn:         func() appthreadmenu.StateProvider { return state }, EffectiveSessionKeyFn: effectiveSessionKey,
		ConversationBackendFn: func() appthreadmenu.ConversationBackendProvider {
			return threadMenuConversationBackendAdapter{
				threadCards:   threadCardInputs{Repository: state, Config: runtimeDeps.cfg, Backend: backend, Conversations: conversations},
				conversations: conversations, runtimeDeps: runtimeDeps,
			}
		},
		BackendRuntimeFn: func() appthreadmenu.BackendRuntimeProvider {
			return threadMenuBackendRuntimeAdapter{deps: runtimeDeps, runtime: backendRuntime(runtimeDeps.view.configuredBackend())}
		},
		PendingQueueFn:    func() appthreadmenu.PendingQueueProvider { return pendingQueue },
		WorkspaceThreadFn: func() appthreadmenu.WorkspaceThreadProvider { return conversations },
		WorkspaceConfigFn: func() appthreadmenu.WorkspaceConfigProvider {
			return threadMenuWorkspaceConfigAdapter{workspaceConfiguration: workspaceConfiguration, backend: backend}
		},
		BackendActionsFn: func() appthreadmenu.BackendActionProvider {
			return threadMenuBackendActionAdapter{service: backendActions}
		},
		BackendDriver:          appbackend.SelectedDriver{Selected: runtimeDeps.view.configuredBackend},
		SessionHasActiveWorkFn: sessionHasActiveWork,
		CancelAutoRetryFn:      autoRetry.Engine.CancelAutoRetry, LockAutoRetryDispatchFn: autoRetryTracker.LockDispatch,
		ReplyCommandActionResponseFn: func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
			return replyCommandActionResponseWith(threadMenuReplyRunner, runtimeDeps.frontendID, threadMenuReplyInThread, msg, resp)
		},
		CommandForkFn: func(msg *feishu.InboundMessage, args []string) error {
			return commandFork(forkDependencies, msg, args)
		},
		CompleteMenuCommandFn: a.CompleteMenuCommand, ActionStringValueFn: actionStringValue,
		MenuCardBodyFn: menuCardBody, MenuCardBodyForBackendFn: menuCardBodyForBackend,
		RenderClaudeSessionPermissionMenuCardFn: permissionMenuRenderer,
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

type threadMenuConfigProvider struct {
	runtime    BackendRuntimeDeps
	store      *state.Store
	workspaces workspace.SelectionService
}

func (p threadMenuConfigProvider) Config() *config.Config  { return p.runtime.cfg }
func (p threadMenuConfigProvider) ConfigMu() *sync.RWMutex { return p.runtime.view.mu }
func (p threadMenuConfigProvider) Backend() string {
	if p.runtime.runtime.owner == nil {
		return p.runtime.view.configuredBackend()
	}
	return p.runtime.runtime.owner.Backend()
}
func (p threadMenuConfigProvider) FrontendID() string { return p.runtime.frontendID }
func (p threadMenuConfigProvider) FrontendConfigIndex() int {
	return p.runtime.view.frontendConfigIndex
}
func (p threadMenuConfigProvider) Store() *state.Store { return p.store }
func (p threadMenuConfigProvider) WorkspaceSelection() workspace.SelectionService {
	return p.workspaces
}

func (a *App) CompleteMenuCommand(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(a, action, sessionKey, rawCommand, parentAction)
}

type threadMenuWorkspaceConfigAdapter struct {
	workspaceConfiguration *workspacecmd.ConfigService
	backend                func() string
}

func (a threadMenuWorkspaceConfigAdapter) CurrentThreadForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace, threadID string, err error) {
	return currentThreadForMessage(a.workspaceConfiguration, a.backend, msg)
}
