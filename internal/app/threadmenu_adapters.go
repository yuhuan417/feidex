package app

import (
	"context"

	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Provider adapters — satisfy threadmenu narrow interfaces
// ---------------------------------------------------------------------------

type threadMenuConversationBackendAdapter struct{ backend conversationBackendFacade }

func (a threadMenuConversationBackendAdapter) RenderThreadsCard(sessionKey string, includeAll bool) (map[string]any, error) {
	return a.backend.renderThreadsCard(sessionKey, includeAll)
}
func (a threadMenuConversationBackendAdapter) InterruptActiveTurn(ctx context.Context, sessionKey string, sess *state.Session) error {
	return a.backend.interruptActiveTurn(ctx, sessionKey, sess)
}
func (a threadMenuConversationBackendAdapter) ContinueActiveTurn(sessionKey string, text string) error {
	return a.backend.continueActiveTurn(sessionKey, text)
}
func (a threadMenuConversationBackendAdapter) ResumeSelectedThread(sessionKey string, sess *state.Session, ws *config.Workspace, selection appthreadmenu.ThreadResumeSelection) (*appthreadmenu.ThreadBinding, error) {
	return a.backend.resumeSelectedThread(sessionKey, sess, ws, threadResumeSelection{
		ThreadID: selection.ThreadID,
		Name:     selection.Name,
		Preview:  selection.Preview,
		Cwd:      selection.Cwd,
	})
}
func (a threadMenuConversationBackendAdapter) ForkReplyMessage(forkedID string) string {
	return a.backend.forkReplyMessage(forkedID)
}

type threadMenuBackendRuntimeAdapter struct {
	app     *App
	runtime backendRuntimeFacade
}

func (a threadMenuBackendRuntimeAdapter) ReconcileCompletedTurnFromFinalOutput(sessionKey string, sess *state.Session) *state.Session {
	if a.runtime == nil {
		return sess
	}
	return a.runtime.reconcileCompletedTurnFromFinalOutput(a.app, sessionKey, sess)
}

func (a threadMenuBackendRuntimeAdapter) ClearActiveOperationsAfterInterrupt(sessionKey string, sess *state.Session) *state.Session {
	if a.runtime == nil {
		return sess
	}
	return a.runtime.clearActiveOperationsAfterInterrupt(a.app, sessionKey, sess)
}

type threadMenuBackendActionAdapter struct {
	app     *App
	facade  backendActionFacade
}

func (a threadMenuBackendActionAdapter) CompleteMenuInterrupt(action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	return a.facade.completeMenuInterrupt(a.app, action, sessionKey, targetTurnID)
}

// ---------------------------------------------------------------------------
// *App methods satisfying threadmenu.App
// ---------------------------------------------------------------------------

func (a *App) ThreadMenuAppState() appthreadmenu.AppStateProvider {
	if a == nil {
		return nil
	}
	return appState(a)
}

func (a *App) ThreadMenuConversationBackend() appthreadmenu.ConversationBackendProvider {
	return threadMenuConversationBackendAdapter{backend: conversationBackend(a)}
}

func (a *App) ThreadMenuBackendRuntime() appthreadmenu.BackendRuntimeProvider {
	return threadMenuBackendRuntimeAdapter{app: a, runtime: backendRuntime(a)}
}

func (a *App) ThreadMenuPendingQueue() appthreadmenu.PendingQueueProvider {
	return newPendingQueueService(a)
}

func (a *App) ThreadMenuWorkspaceThread() appthreadmenu.WorkspaceThreadProvider {
	return newWorkspaceThreadService(a)
}

func (a *App) ThreadMenuWorkspaceConfig() appthreadmenu.WorkspaceConfigProvider {
	return newWorkspaceConfigService(a)
}

func (a *App) ThreadMenuBackendActions() appthreadmenu.BackendActionProvider {
	actions := backendActions(a)
	if actions == nil {
		return nil
	}
	return threadMenuBackendActionAdapter{app: a, facade: actions}
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

func (a *App) CommandLabel(label, slash string) string {
	return commandLabel(label, slash)
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

func (a *App) RenderClaudeSessionPermissionMenuCard(sessionKey string) (map[string]any, error) {
	return renderClaudeSessionPermissionMenuCard(a, sessionKey)
}

func (a *App) ShowClaudeSessionPermissionMenuFromApp(msg *feishu.InboundMessage) error {
	return showClaudeSessionPermissionMenu(a, msg)
}
