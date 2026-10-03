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

type debugOutbound struct{ app *App }

func (o debugOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return replyCardWithIDEffect(ctx, o.app, messageID, card, inThread)
}
func (o debugOutbound) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	return replyTextByAnchorEffect(ctx, o.app, messageID, text, inThread)
}
func (o debugOutbound) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	return patchCardEffect(ctx, o.app, messageID, card)
}

type debugArtifactSharer struct{ app *App }

func (o debugArtifactSharer) Share(ctx context.Context, req fileshare.Request) (fileshare.Result, error) {
	if o.app == nil || o.app.feishu == nil {
		return fileshare.Result{}, context.Canceled
	}
	result, err := o.app.feishu.ShareLocalFile(ctx, feishu.SharedFileRequest{LocalPath: req.LocalPath, ChatID: req.ChatID, UserID: req.UserID})
	return fileshare.Result{FileName: result.FileName, URL: result.URL, SizeBytes: result.SizeBytes}, err
}

func FileSharePorts(a *App) (fileshare.Artifacts, fileshare.Presentation, func(string, func()) bool) {
	return debugArtifactSharer{app: a}, appdebugviewcmd.DownloadPresentation{Dependencies: newDebugViewAppAdapter(a)}, func(key string, fn func()) bool {
		return a.runtimeOwner.Lifecycle.Run(func() { runSession(a, key, fn) }, a.asyncRunner)
	}
}

type debugCardRenderer struct{ app *App }

func (o debugCardRenderer) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	if o.app == nil || o.app.feishu == nil {
		return nil
	}
	return o.app.feishu.SimpleStatusCard(title, color, body, buttons)
}

func newDebugViewAppAdapter(app *App) appdebugviewcmd.Dependencies {
	if app == nil {
		return appdebugviewcmd.Dependencies{}
	}
	return appdebugviewcmd.Dependencies{
		ConfigProvider: app, ContextProvider: app, RuntimeConfigRepository: configadapter.NewRuntimeRepository(app), Outbound: debugOutbound{app: app}, FileSharing: app.bindings.FileSharing, CardRenderer: debugCardRenderer{app: app}, StateProvider: app.State(),
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
	return currentWorkspaceForMessage(a.app, msg)
}

type debugWorkspaceRenderAdapter struct {
	app *App
}

func (a debugWorkspaceRenderAdapter) RenderPathPickerCard(requestID string, payload appdebugviewcmd.PathPickerPayload) (map[string]any, error) {
	return a.app.bindings.WorkspacePresentation.RenderPathPickerCard(requestID, payload)
}
