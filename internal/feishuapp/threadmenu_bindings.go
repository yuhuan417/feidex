package feishuapp

import (
	retryview "feidex/internal/adapter/feishu/autoretry"
	appstate "feidex/internal/adapter/storage/json/scoped"
	appsubmission "feidex/internal/application/submission"
	"feidex/internal/application/threadsettings"
	"feidex/internal/domain/conversation"
	backendruntime "feidex/internal/runtime"
	"feidex/internal/state"

	"context"
	appbackend "feidex/internal/adapter/feishu/backend"

	appthreadmenu "feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/adapter/feishu/workspacecmd"
	conversationapp "feidex/internal/application/conversation"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type ThreadMenuInputs struct {
	Runtime                BackendRuntimeDeps
	Store                  *state.Store
	State                  *appstate.Store
	Conversations          *conversationapp.Service
	BindingScope           BindingScope
	ConversationQuery      conversationapp.Query
	PendingQueue           *appsubmission.PendingQueueService
	WorkspaceConfiguration *workspacecmd.ConfigService
	WorkspaceSelection     workspaceapp.SelectionService
	ConversationControls   *conversationapp.Controls
	ThreadSettings         threadsettings.Service
	PermissionSettings     threadsettings.PermissionService
	BackendActions         appbackend.ActionService
	AutoRetry              retryview.Service
	CompleteMenuCommand    func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
}

func BuildThreadMenuDependencies(inputs ThreadMenuInputs) appthreadmenu.Dependencies {
	runtimeDeps := inputs.Runtime
	owner := runtimeDeps.runtime.owner
	if owner == nil {
		return appthreadmenu.Dependencies{}
	}
	runtimeDeps.contextFn = owner.Lifecycle.Context
	backend := ConfiguredBackendBuilder(runtimeDeps.cfg, runtimeDeps.view.mu, owner.Backend, runtimeDeps.frontendID, runtimeDeps.view.frontendConfigIndex)
	state := inputs.State
	forkConfigView := frontendConfigView{
		cfg: runtimeDeps.cfg, mu: runtimeDeps.view.mu,
		frontendID: runtimeDeps.frontendID, frontendConfigIndex: runtimeDeps.view.frontendConfigIndex,
	}
	conversations := inputs.Conversations
	bindingScope := inputs.BindingScope
	conversationQuery := inputs.ConversationQuery
	pendingQueue := inputs.PendingQueue
	workspaceConfiguration := inputs.WorkspaceConfiguration
	workspaceSelection := inputs.WorkspaceSelection
	conversationControls := inputs.ConversationControls
	threadSettings := inputs.ThreadSettings
	permissionSettings := inputs.PermissionSettings
	backendActions := inputs.BackendActions
	autoRetry := inputs.AutoRetry
	effectiveSessionKey := func(sessionKey string) string {
		return threadMenuEffectiveSessionKey(runtimeDeps.view.normalizeSessionKey, bindingScope.scope, conversationQuery, sessionKey)
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
		ConfigProvider: newFrontendConfigProvider(runtimeDeps, inputs.Store, workspaceSelection), Outbound: newEffectOutbound(runtimeDeps.frontendID, newEffectRunner(owner)), Controls: conversationControls, Settings: threadSettings,
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
		CompleteMenuCommandFn: inputs.CompleteMenuCommand, ActionStringValueFn: actionStringValue,
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
