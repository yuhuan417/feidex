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

type debugViewAppAdapter struct{ *App }

func newDebugViewAppAdapter(app *App) debugViewAppAdapter {
	return debugViewAppAdapter{App: app}
}

func newDebugServiceInner(app *App) appdebugviewcmd.DebugService {
	return appdebugviewcmd.NewDebugService(newDebugViewAppAdapter(app))
}

func newUsageServiceInner(app *App) appdebugviewcmd.UsageService {
	return appdebugviewcmd.NewUsageService(newDebugViewAppAdapter(app))
}

func (a debugViewAppAdapter) DebugFeishu() appdebugviewcmd.FeishuClient {
	return a.feishu
}

func (a debugViewAppAdapter) DebugAppState() appdebugviewcmd.AppStateProvider {
	return a.State()
}

func (a debugViewAppAdapter) DebugRuntimeState() appdebugviewcmd.RuntimeStateProvider {
	return debugRuntimeStateAdapter{app: a.App}
}

func (a debugViewAppAdapter) DebugConversationBackend() appdebugviewcmd.ConversationBackendProvider {
	return debugConversationBackendAdapter{app: a.App}
}

func (a debugViewAppAdapter) DebugWorkspaceConfig() appdebugviewcmd.WorkspaceConfigProvider {
	return debugWorkspaceConfigAdapter{app: a.App}
}

func (a debugViewAppAdapter) DebugWorkspaceRender() appdebugviewcmd.WorkspaceRenderProvider {
	return debugWorkspaceRenderAdapter{app: a.App}
}

func (a debugViewAppAdapter) DebugMakeSessionKey(msg *feishu.InboundMessage) string {
	return makeSessionKey(a.App, msg)
}

func (a debugViewAppAdapter) DebugReplyInThreadEnabled(chatType string) bool {
	return replyInThreadEnabled(a.App, chatType)
}

func (a debugViewAppAdapter) DebugCompleteMenuCommand(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(a.App, action, sessionKey, rawCommand, parentAction)
}

func (a debugViewAppAdapter) DebugMenuCardBody(action, body string) string {
	return menuCardBody(action, body)
}

func (a debugViewAppAdapter) DebugMenuBreadcrumbLabels(action string) []string {
	return menuBreadcrumbLabels(action)
}

func (a debugViewAppAdapter) DebugCommandLabel(label, slash string) string {
	return commandLabel(label, slash)
}

func (a debugViewAppAdapter) DebugCurrentThreadLabel(sess *state.Session) string {
	return appthreadmenu.SessionCurrentThreadLabel(sess)
}

func (a debugViewAppAdapter) DebugPrimaryConversationMissingLabel(backend string) string {
	return primaryConversationMissingLabel(backend)
}

func (a debugViewAppAdapter) DebugDefaultWorkspaceID() string {
	return defaultWorkspaceID(a.App)
}

func (a debugViewAppAdapter) DebugConfigPath() string {
	return a.cfgPath
}

type debugRuntimeStateAdapter struct {
	app *App
}

func (a debugRuntimeStateAdapter) TurnBindingTracker() appdebugviewcmd.TurnBindingTracker {
	return newRuntimeStateService(a.app).turnBindingTracker()
}

func (a debugRuntimeStateAdapter) CurrentThreadUsage(threadID string) (codexrpc.ThreadTokenUsage, bool) {
	return newRuntimeStateService(a.app).currentThreadUsage(threadID)
}

type debugConversationBackendAdapter struct {
	app *App
}

func (a debugConversationBackendAdapter) RenderUsageBody(sess *state.Session) string {
	return conversationBackend(a.app).RenderUsageBody(sess)
}

type debugWorkspaceConfigAdapter struct {
	app *App
}

func (a debugWorkspaceConfigAdapter) CurrentWorkspaceForMessage(msg *feishu.InboundMessage) (string, *state.Session, *config.Workspace) {
	return currentWorkspaceForMessage(a.app, msg)
}

type debugWorkspaceRenderAdapter struct {
	app *App
}

func (a debugWorkspaceRenderAdapter) RenderPathPickerCard(requestID string, payload appdebugviewcmd.PathPickerPayload) (map[string]any, error) {
	return newWorkspaceRenderServiceInner(a.app).RenderPathPickerCard(requestID, payload)
}
