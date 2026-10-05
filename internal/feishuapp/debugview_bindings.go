package feishuapp

import (
	"context"
	configadapter "feidex/internal/adapter/config"
	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	appthreadmenu "feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/adapter/feishu/workspacecmd"
	"feidex/internal/application/fileshare"
	"feidex/internal/config"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	domainturn "feidex/internal/domain/turn"
	domainworkspace "feidex/internal/domain/workspace"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	"strings"

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

func FileSharePorts(client debugFileSharer, dependencies appdebugviewcmd.Dependencies, lifecycle *frontendruntime.FrontendRuntime, actors *frontendruntime.SessionActors, asyncRunner func(func())) (fileshare.Artifacts, fileshare.Presentation, func(string, func()) bool) {
	return debugArtifactSharer{client: client}, appdebugviewcmd.DownloadPresentation{Dependencies: dependencies}, func(key string, fn func()) bool {
		if lifecycle == nil {
			return false
		}
		return lifecycle.Run(func() { runSessionOnActor(actors, key, fn) }, asyncRunner)
	}
}

func DebugViewDependencies(app *App) appdebugviewcmd.Dependencies {
	if app == nil {
		return appdebugviewcmd.Dependencies{}
	}
	configProvider := newFrontendConfigProvider(app.BackendRuntimeDeps(), app.store, app.bindings.WorkspaceSelection)
	return appdebugviewcmd.Dependencies{
		ConfigProvider: configProvider, ContextProvider: app, RuntimeConfigRepository: configadapter.NewRuntimeRepository(configProvider), Outbound: newEffectOutbound(app.FrontendID(), newEffectRunner(app.runtimeOwner)), FileSharing: app.bindings.FileSharing, CardRenderer: simpleStatusCardRenderer{client: app.feishu}, StateProvider: app.State(),
		RuntimeStateProvider: debugRuntimeStateAdapter{tracker: app.runtimeOwner.TurnBindings},
		ConversationBackendProvider: debugConversationBackendAdapter{
			backend:      ConfiguredBackendBuilder(app.Config(), app.ConfigMu(), app.runtimeOwner.Backend, app.FrontendID(), app.frontendConfigIndex),
			runtimeState: debugRuntimeStateAdapter{tracker: app.runtimeOwner.TurnBindings},
			threadLabel:  appthreadmenu.SessionCurrentThreadLabel,
			missingLabel: primaryConversationMissingLabel,
		},
		WorkspaceConfigProvider: debugWorkspaceConfigAdapter{configuration: app.bindings.WorkspaceConfiguration},
		WorkspaceRenderProvider: debugWorkspaceRenderAdapter{render: app.bindings.WorkspacePresentation.RenderPathPickerCard},
		MakeSessionKeyFn:        func(m *feishu.InboundMessage) string { return app.configView().makeSessionKey(m) }, ReplyInThreadEnabledFn: func(v string) bool { return app.configView().replyInThreadEnabled() },
		CompleteMenuCommandFn: func(a *feishu.CardAction, s, r, p string) (*callback.CardActionTriggerResponse, error) {
			return completeMenuCommand(app, a, s, r, p)
		},
		MenuCardBodyFn: menuCardBody, MenuBreadcrumbLabelsFn: menuBreadcrumbLabels, CommandLabelFn: commandLabel,
		CurrentThreadLabelFn: appthreadmenu.SessionCurrentThreadLabel, PrimaryConversationMissingLabelFn: primaryConversationMissingLabel,
		DefaultWorkspaceIDFn: func() string { return app.configView().defaultWorkspaceID() }, ConfigPathFn: func() string { return app.cfgPath },
	}
}

func BuildDebug(dependencies appdebugviewcmd.Dependencies) appdebugviewcmd.DebugService {
	return appdebugviewcmd.NewDebugService(dependencies)
}

func BuildUsage(dependencies appdebugviewcmd.Dependencies) appdebugviewcmd.UsageService {
	return appdebugviewcmd.NewUsageService(dependencies)
}

type debugTurnBindingTracker interface {
	appdebugviewcmd.TurnBindingTracker
	CurrentThreadUsage(string) (domainturn.ThreadTokenUsage, bool)
}

type debugRuntimeStateAdapter struct{ tracker debugTurnBindingTracker }

func (a debugRuntimeStateAdapter) TurnBindingTracker() appdebugviewcmd.TurnBindingTracker {
	return a.tracker
}

func (a debugRuntimeStateAdapter) CurrentThreadUsage(threadID string) (domainturn.ThreadTokenUsage, bool) {
	return a.tracker.CurrentThreadUsage(threadID)
}

type debugConversationBackendAdapter struct {
	backend      func() string
	runtimeState debugRuntimeStateAdapter
	threadLabel  func(*conversation.Session) string
	missingLabel func(string) string
}

func (a debugConversationBackendAdapter) RenderUsageBody(sess *conversation.Session) string {
	backend := a.backend()
	if backend == domainbackend.BackendClaude {
		if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
			return a.missingLabel(domainbackend.BackendClaude) + "。"
		}
		body := "当前会话暂无 Claude usage 数据。"
		if a.runtimeState.tracker != nil {
			if usage, ok := a.runtimeState.tracker.GetClaudeThreadUsage(sess.ActiveThreadID); ok {
				body = appdebugviewcmd.RenderClaudeThreadUsageCardBody(a.threadLabel(sess), sess.ActiveThreadID, usage)
			}
		}
		return body
	}
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return a.missingLabel(domainbackend.BackendCodex) + "。"
	}
	body := "当前线程暂无 token usage 数据。"
	if usage, ok := a.runtimeState.CurrentThreadUsage(sess.ActiveThreadID); ok {
		contextLine := ""
		if usage.ModelContextWindow != nil {
			contextLine = appdebugviewcmd.FormatContextLeftLine(usage.Last.InputTokens, *usage.ModelContextWindow)
		}
		body = appdebugviewcmd.RenderThreadUsageCardBody(a.threadLabel(sess), sess.ActiveThreadID, usage, contextLine)
	}
	return body
}

type debugWorkspaceConfigAdapter struct{ configuration *workspacecmd.ConfigService }

func (a debugWorkspaceConfigAdapter) CurrentWorkspaceForMessage(msg *feishu.InboundMessage) (string, *conversation.Session, *config.Workspace) {
	return currentWorkspaceForMessage(a.configuration, msg)
}

type debugWorkspaceRenderAdapter struct {
	render func(string, domainworkspace.PathPickerPayload) (map[string]any, error)
}

func (a debugWorkspaceRenderAdapter) RenderPathPickerCard(requestID string, payload appdebugviewcmd.PathPickerPayload) (map[string]any, error) {
	return a.render(requestID, payload)
}
