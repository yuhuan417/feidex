package app

import (
	appdebugviewcmd "feidex/internal/app/debugviewcmd"
	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newDebugViewAppAdapter(app *App) appdebugviewcmd.Dependencies {
	if app == nil {
		return appdebugviewcmd.Dependencies{}
	}
	return appdebugviewcmd.Dependencies{
		ConfigProvider: app, FeishuClient: app.feishu, StateProvider: app.State(),
		RuntimeStateProvider: debugRuntimeStateAdapter{app: app}, ConversationBackendProvider: debugConversationBackendAdapter{app: app},
		WorkspaceConfigProvider: debugWorkspaceConfigAdapter{app: app}, WorkspaceRenderProvider: debugWorkspaceRenderAdapter{app: app},
		MakeSessionKeyFn: func(m *feishu.InboundMessage) string { return makeSessionKey(app, m) }, ReplyInThreadEnabledFn: func(v string) bool { return replyInThreadEnabled(app, v) },
		CompleteMenuCommandFn: func(a *feishu.CardAction, s, r, p string) (*callback.CardActionTriggerResponse, error) {
			return completeMenuCommand(app, a, s, r, p)
		},
		MenuCardBodyFn: menuCardBody, MenuBreadcrumbLabelsFn: menuBreadcrumbLabels, CommandLabelFn: commandLabel,
		CurrentThreadLabelFn: appthreadmenu.SessionCurrentThreadLabel, PrimaryConversationMissingLabelFn: primaryConversationMissingLabel,
		DefaultWorkspaceIDFn: func() string { return defaultWorkspaceID(app) }, ConfigPathFn: func() string { return app.cfgPath },
	}
}

func newDebugService(app *App) appdebugviewcmd.DebugService {
	return appdebugviewcmd.NewDebugService(newDebugViewAppAdapter(app))
}

func newUsageService(app *App) appdebugviewcmd.UsageService {
	return appdebugviewcmd.NewUsageService(newDebugViewAppAdapter(app))
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

func (a debugConversationBackendAdapter) RenderUsageBody(sess *conversation.Session) string {
	return renderConversationUsage(a.app, sess)
}

type debugWorkspaceConfigAdapter struct {
	app *App
}

func (a debugWorkspaceConfigAdapter) CurrentWorkspaceForMessage(msg *feishu.InboundMessage) (string, *conversation.Session, *config.Workspace) {
	return currentWorkspaceForMessage(a.app, msg)
}

type debugWorkspaceRenderAdapter struct {
	app *App
}

func (a debugWorkspaceRenderAdapter) RenderPathPickerCard(requestID string, payload appdebugviewcmd.PathPickerPayload) (map[string]any, error) {
	return newWorkspaceRenderService(a.app).RenderPathPickerCard(requestID, payload)
}
