package feishuapp

import (
	"context"
	configadapter "feidex/internal/adapter/config"
	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	appthreadmenu "feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/application/fileshare"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	domainturn "feidex/internal/domain/turn"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type debugFileSharer interface {
	ShareLocalFile(context.Context, feishu.SharedFileRequest) (feishu.SharedFileResult, error)
}

type debugArtifactSharer struct{ client debugFileSharer }

func (o debugArtifactSharer) Share(ctx context.Context, req fileshare.Request) (fileshare.Result, error) {
	if o.client == nil {
		return fileshare.Result{}, context.Canceled
	}
	result, err := o.client.ShareLocalFile(ctx, feishu.SharedFileRequest{LocalPath: req.LocalPath, ChatID: req.ChatID, UserID: req.UserID})
	return fileshare.Result{FileName: result.FileName, URL: result.URL, SizeBytes: result.SizeBytes}, err
}

func FileSharePorts(a *App) (fileshare.Artifacts, fileshare.Presentation, func(string, func()) bool) {
	var client debugFileSharer
	if a != nil {
		client = a.feishu
	}
	return debugArtifactSharer{client: client}, appdebugviewcmd.DownloadPresentation{Dependencies: newDebugViewAppAdapter(a)}, func(key string, fn func()) bool {
		return a.runtimeOwner.Lifecycle.Run(func() { runSession(a, key, fn) }, a.asyncRunner)
	}
}

func newDebugViewAppAdapter(app *App) appdebugviewcmd.Dependencies {
	if app == nil {
		return appdebugviewcmd.Dependencies{}
	}
	return appdebugviewcmd.Dependencies{
		ConfigProvider: app, ContextProvider: app, RuntimeConfigRepository: configadapter.NewRuntimeRepository(app), Outbound: newEffectOutbound(app.FrontendID(), newEffectRunner(app.runtimeOwner)), FileSharing: app.bindings.FileSharing, CardRenderer: simpleStatusCardRenderer{client: app.feishu}, StateProvider: app.State(),
		RuntimeStateProvider: debugRuntimeStateAdapter{app: app}, ConversationBackendProvider: debugConversationBackendAdapter{app: app},
		WorkspaceConfigProvider: debugWorkspaceConfigAdapter{app: app}, WorkspaceRenderProvider: debugWorkspaceRenderAdapter{app: app},
		MakeSessionKeyFn: func(m *feishu.InboundMessage) string { return app.configView().makeSessionKey(m) }, ReplyInThreadEnabledFn: func(v string) bool { return app.configView().replyInThreadEnabled() },
		CompleteMenuCommandFn: func(a *feishu.CardAction, s, r, p string) (*callback.CardActionTriggerResponse, error) {
			return completeMenuCommand(app, a, s, r, p)
		},
		MenuCardBodyFn: menuCardBody, MenuBreadcrumbLabelsFn: menuBreadcrumbLabels, CommandLabelFn: commandLabel,
		CurrentThreadLabelFn: appthreadmenu.SessionCurrentThreadLabel, PrimaryConversationMissingLabelFn: primaryConversationMissingLabel,
		DefaultWorkspaceIDFn: func() string { return app.configView().defaultWorkspaceID() }, ConfigPathFn: func() string { return app.cfgPath },
	}
}

func BuildDebug(app *App) appdebugviewcmd.DebugService {
	return appdebugviewcmd.NewDebugService(newDebugViewAppAdapter(app))
}

func BuildUsage(app *App) appdebugviewcmd.UsageService {
	return appdebugviewcmd.NewUsageService(newDebugViewAppAdapter(app))
}

type debugRuntimeStateAdapter struct {
	app *App
}

func (a debugRuntimeStateAdapter) TurnBindingTracker() appdebugviewcmd.TurnBindingTracker {
	return a.app.runtimeOwner.TurnBindings
}

func (a debugRuntimeStateAdapter) CurrentThreadUsage(threadID string) (domainturn.ThreadTokenUsage, bool) {
	return a.app.runtimeOwner.TurnBindings.CurrentThreadUsage(threadID)
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
	return currentWorkspaceForMessage(a.app.bindings.WorkspaceConfiguration, msg)
}

type debugWorkspaceRenderAdapter struct {
	app *App
}

func (a debugWorkspaceRenderAdapter) RenderPathPickerCard(requestID string, payload appdebugviewcmd.PathPickerPayload) (map[string]any, error) {
	return a.app.bindings.WorkspacePresentation.RenderPathPickerCard(requestID, payload)
}
