package app

import (
	appdebugviewcmd "feidex/internal/app/debugviewcmd"
	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Adapter methods on *App — satisfy debugviewcmd.App interface
// ---------------------------------------------------------------------------

// DebugFeishu returns the Feishu bot client for the debug/usage/download services.
func (a *App) DebugFeishu() appdebugviewcmd.FeishuClient {
	return a.feishu
}

// DebugAppState returns the narrowed app state provider for debug/usage/download ops.
func (a *App) DebugAppState() appdebugviewcmd.AppStateProvider {
	return a.State()
}

// DebugRuntimeState returns the narrowed runtime state provider for usage ops.
func (a *App) DebugRuntimeState() appdebugviewcmd.RuntimeStateProvider {
	return debugRuntimeStateAdapter{app: a}
}

// debugRuntimeStateAdapter wraps runtimeStateService to satisfy
// debugviewcmd.RuntimeStateProvider (return type mismatch on TurnBindingTracker).
type debugRuntimeStateAdapter struct {
	app *App
}

func (a debugRuntimeStateAdapter) TurnBindingTracker() appdebugviewcmd.TurnBindingTracker {
	return newRuntimeStateService(a.app).turnBindingTracker()
}

func (a debugRuntimeStateAdapter) CurrentThreadUsage(threadID string) (codexrpc.ThreadTokenUsage, bool) {
	return newRuntimeStateService(a.app).currentThreadUsage(threadID)
}

// DebugConversationBackend returns the narrowed conversation backend provider
// for usage rendering.
func (a *App) DebugConversationBackend() appdebugviewcmd.ConversationBackendProvider {
	return debugConversationBackendAdapter{app: a}
}

// DebugWorkspaceConfig returns the narrowed workspace config provider.
func (a *App) DebugWorkspaceConfig() appdebugviewcmd.WorkspaceConfigProvider {
	return debugWorkspaceConfigAdapter{app: a}
}

// DebugWorkspaceRender returns the narrowed workspace render provider.
func (a *App) DebugWorkspaceRender() appdebugviewcmd.WorkspaceRenderProvider {
	return debugWorkspaceRenderAdapter{app: a}
}

// DebugMakeSessionKey builds a session key from an inbound message.
func (a *App) DebugMakeSessionKey(msg *feishu.InboundMessage) string {
	return makeSessionKey(a, msg)
}

// DebugReplyInThreadEnabled reports whether reply-in-thread is enabled.
func (a *App) DebugReplyInThreadEnabled(chatType string) bool {
	return replyInThreadEnabled(a, chatType)
}

// DebugCompleteMenuCommand dispatches a menu command from a card action.
func (a *App) DebugCompleteMenuCommand(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(a, action, sessionKey, rawCommand, parentAction)
}

// DebugMenuCardBody formats a menu card body with breadcrumb navigation.
func (a *App) DebugMenuCardBody(action, body string) string {
	return menuCardBody(action, body)
}

// DebugMenuBreadcrumbLabels returns breadcrumb labels for a menu action.
func (a *App) DebugMenuBreadcrumbLabels(action string) []string {
	return menuBreadcrumbLabels(action)
}

// DebugCommandLabel formats a command label with its slash command.
func (a *App) DebugCommandLabel(label, slash string) string {
	return commandLabel(label, slash)
}

// DebugCurrentThreadLabel returns the display label for the active thread.
func (a *App) DebugCurrentThreadLabel(sess *state.Session) string {
	return appthreadmenu.SessionCurrentThreadLabel(sess)
}

// DebugPrimaryConversationMissingLabel returns the missing conversation label
// for the given backend.
func (a *App) DebugPrimaryConversationMissingLabel(backend string) string {
	return primaryConversationMissingLabel(backend)
}

// DebugDefaultWorkspaceID returns the default workspace ID.
func (a *App) DebugDefaultWorkspaceID() string {
	return defaultWorkspaceID(a)
}

// DebugConfigPath returns the config file path.
func (a *App) DebugConfigPath() string {
	return a.cfgPath
}

// debugConversationBackendAdapter wraps the conversation backend facade to
// satisfy debugviewcmd.ConversationBackendProvider.
type debugConversationBackendAdapter struct {
	app *App
}

func (a debugConversationBackendAdapter) RenderUsageBody(sess *state.Session) string {
	return conversationBackend(a.app).RenderUsageBody(sess)
}

// debugWorkspaceConfigAdapter wraps workspace config operations to satisfy
// debugviewcmd.WorkspaceConfigProvider.
type debugWorkspaceConfigAdapter struct {
	app *App
}

func (a debugWorkspaceConfigAdapter) CurrentWorkspaceForMessage(msg *feishu.InboundMessage) (string, *state.Session, *config.Workspace) {
	return newWorkspaceConfigService(a.app).currentWorkspaceForMessage(msg)
}

// debugWorkspaceRenderAdapter wraps workspace render operations to satisfy
// debugviewcmd.WorkspaceRenderProvider.
type debugWorkspaceRenderAdapter struct {
	app *App
}

func (a debugWorkspaceRenderAdapter) RenderPathPickerCard(requestID string, payload appdebugviewcmd.PathPickerPayload) (map[string]any, error) {
	return newWorkspaceRenderService(a.app).renderPathPickerCard(requestID, payload)
}
