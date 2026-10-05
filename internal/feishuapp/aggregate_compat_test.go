package feishuapp

import (
	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/feishu"
)

func menuCommandServiceForApp(a *App) MenuCommandService {
	if a == nil {
		return MenuCommandService{}
	}
	return NewMenuCommandService(MenuCommandInputs{
		BindingScope: BindingScope{scope: a.bindings.BindingCommands.scope},
		Capture:      a.feishu,
		HandleCommand: func(msg *feishu.InboundMessage, raw string) error {
			return HandleInboundCommand(a, msg, raw)
		},
		RenderFallback: func(actionName, sessionKey string) (map[string]any, bool) {
			return RenderMenuCommandFallback(a, actionName, sessionKey)
		},
	})
}

func asyncCardActionServiceForApp(a *App) AsyncCardActionService {
	if a == nil {
		return AsyncCardActionService{}
	}
	return NewAsyncCardActionService(AsyncCardActionInputs{
		Commands: menuCommandServiceForApp(a), Lifecycle: &a.runtimeOwner.Lifecycle,
		AsyncRunner: a.asyncRunner, Actors: a.runtimeOwner.SessionActors, Context: a.Context,
		FrontendID: a.FrontendID(), Effects: newEffectRunner(a.runtimeOwner),
	})
}

func DebugViewDependencies(a *App) appdebugviewcmd.Dependencies {
	return BuildDebugViewDependencies(a, menuCommandServiceForApp(a).Complete)
}

func ReviewCommandDependencies(a *App) appreviewcmd.Dependencies {
	return BuildReviewCommandDependencies(a, asyncCardActionServiceForApp(a))
}

func BuildWorkspaceConfiguration(a *App, presentation *workspacecards.Presentation, conversations *conversationapp.Service) *appworkspacecmd.ConfigService {
	return BuildWorkspaceConfigurationWithMenu(a, presentation, conversations, menuCommandServiceForApp(a).Complete)
}

func BuildWorkspaceManagement(a *App, presentation *workspacecards.Presentation, conversations *conversationapp.Service, scope BindingScope) *appworkspacecmd.ManagementService {
	return BuildWorkspaceManagementWithMenu(a, presentation, conversations, scope, menuCommandServiceForApp(a).Complete)
}
