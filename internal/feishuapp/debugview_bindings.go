package feishuapp

import (
	"context"
	configadapter "feidex/internal/adapter/config"
	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	appthreadmenu "feidex/internal/adapter/feishu/threadmenu"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	"feidex/internal/adapter/feishu/workspacecmd"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/fileshare"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/config"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	domainturn "feidex/internal/domain/turn"
	domainworkspace "feidex/internal/domain/workspace"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"
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

type DebugViewInputs struct {
	Runtime                BackendRuntimeDeps
	Store                  *state.Store
	WorkspaceSelection     workspaceapp.SelectionService
	Feishu                 FeishuClient
	FileSharing            *fileshare.Service
	State                  *appstate.Store
	TurnBindings           debugTurnBindingTracker
	WorkspaceConfiguration *workspacecmd.ConfigService
	WorkspacePresentation  *workspacecards.Presentation
	Effects                frontendruntime.EffectRunner
	CompleteMenuCommand    func(*feishu.CardAction, string, string, string) (*callback.CardActionTriggerResponse, error)
}

func BuildDebugViewDependencies(inputs DebugViewInputs) appdebugviewcmd.Dependencies {
	runtimeDeps := inputs.Runtime
	configProvider := newFrontendConfigProvider(runtimeDeps, inputs.Store, inputs.WorkspaceSelection)
	backend := runtimeDeps.view.configuredBackend
	if owner := runtimeDeps.runtime.owner; owner != nil {
		backend = ConfiguredBackendBuilder(runtimeDeps.cfg, runtimeDeps.view.mu, owner.Backend, runtimeDeps.frontendID, runtimeDeps.view.frontendConfigIndex)
	}
	runtimeState := debugRuntimeStateAdapter{tracker: inputs.TurnBindings}
	return appdebugviewcmd.Dependencies{
		ConfigProvider: configProvider, ContextProvider: configProvider, RuntimeConfigRepository: configadapter.NewRuntimeRepository(configProvider), Outbound: newEffectOutbound(runtimeDeps.frontendID, inputs.Effects), FileSharing: inputs.FileSharing, CardRenderer: simpleStatusCardRenderer{client: inputs.Feishu}, StateProvider: inputs.State,
		RuntimeStateProvider: runtimeState,
		ConversationBackendProvider: debugConversationBackendAdapter{
			backend:      backend,
			runtimeState: runtimeState,
			threadLabel:  appthreadmenu.SessionCurrentThreadLabel,
			missingLabel: primaryConversationMissingLabel,
		},
		WorkspaceConfigProvider: debugWorkspaceConfigAdapter{configuration: inputs.WorkspaceConfiguration},
		WorkspaceRenderProvider: debugWorkspaceRenderAdapter{render: inputs.WorkspacePresentation.RenderPathPickerCard},
		MakeSessionKeyFn:        runtimeDeps.view.makeSessionKey, ReplyInThreadEnabledFn: func(string) bool { return runtimeDeps.view.replyInThreadEnabled() },
		CompleteMenuCommandFn: inputs.CompleteMenuCommand,
		MenuCardBodyFn:        menuCardBody, MenuBreadcrumbLabelsFn: menuBreadcrumbLabels, CommandLabelFn: commandLabel,
		CurrentThreadLabelFn: appthreadmenu.SessionCurrentThreadLabel, PrimaryConversationMissingLabelFn: primaryConversationMissingLabel,
		DefaultWorkspaceIDFn: runtimeDeps.view.defaultWorkspaceID, ConfigPathFn: func() string { return runtimeDeps.cfgPath },
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
