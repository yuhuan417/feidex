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
	if a == nil {
		return appdebugviewcmd.Dependencies{}
	}
	return BuildDebugViewDependencies(DebugViewInputs{
		Runtime: a.BackendRuntimeDeps(), Store: a.store, WorkspaceSelection: a.bindings.WorkspaceSelection,
		Feishu: a.Feishu(), FileSharing: a.bindings.FileSharing, State: a.State(),
		TurnBindings: a.runtimeOwner.TurnBindings, WorkspaceConfiguration: a.bindings.WorkspaceConfiguration,
		WorkspacePresentation: a.bindings.WorkspacePresentation, Effects: newEffectRunner(a.runtimeOwner),
		CompleteMenuCommand: menuCommandServiceForApp(a).Complete,
	})
}

func ReviewCommandDependencies(a *App) appreviewcmd.Dependencies {
	if a == nil {
		return appreviewcmd.Dependencies{}
	}
	return BuildReviewCommandDependencies(ReviewCommandInputs{
		Runtime: a.BackendRuntimeDeps(), Store: a.store, WorkspaceSelection: a.bindings.WorkspaceSelection,
		UseCase: a.bindings.Review, Submissions: a.bindings.Submissions,
		BindingScope: BindingScope{scope: a.bindings.BindingCommands.scope}, PendingQueue: a.bindings.PendingQueue,
		QueuedNotice: a.bindings.OutboundCards, Feishu: a.Feishu(), State: a.State(),
		Effects: newEffectRunner(a.runtimeOwner), AsyncActions: asyncCardActionServiceForApp(a),
	})
}

func BuildWorkspaceConfiguration(a *App, presentation *workspacecards.Presentation, conversations *conversationapp.Service) *appworkspacecmd.ConfigService {
	if a == nil {
		return appworkspacecmd.NewConfigService(appworkspacecmd.ConfigDeps{})
	}
	dependencies := workspaceCommandDependenciesForApp(a, presentation)
	return BuildWorkspaceConfigurationService(WorkspaceConfigurationInputs{
		Dependencies: dependencies, State: a.State(), LiveThreads: a.runtimeOwner.LiveThreads,
		Conversations: conversations, Presentation: presentation,
		CompleteMenuCommand: menuCommandServiceForApp(a).Complete, FrontendID: a.FrontendID(),
		Effects: newEffectRunner(a.runtimeOwner), ReplyInThread: a.configView().replyInThreadEnabled(),
	})
}

func BuildWorkspaceManagement(a *App, presentation *workspacecards.Presentation, conversations *conversationapp.Service, scope BindingScope) *appworkspacecmd.ManagementService {
	if a == nil {
		return appworkspacecmd.NewManagementService(appworkspacecmd.ManagementDeps{})
	}
	return BuildWorkspaceManagementService(WorkspaceManagementInputs{
		Dependencies: workspaceCommandDependenciesForApp(a, presentation), State: a.State(), RuntimeOwner: a.runtimeOwner,
		AsyncRunner: a.asyncRunner, Conversations: conversations, BindingScope: scope,
		AnnouncementQuery: a.bindings.AnnouncementQuery, CompleteMenuCommand: menuCommandServiceForApp(a).Complete,
		FrontendID: a.FrontendID(), Effects: newEffectRunner(a.runtimeOwner),
		ReplyInThread: a.configView().replyInThreadEnabled(), Presentation: presentation,
	})
}

func workspaceCommandDependenciesForApp(a *App, presentation *workspacecards.Presentation) appworkspacecmd.Dependencies {
	return BuildWorkspaceCommandDependencies(WorkspaceCommandInputs{
		Runtime: a.BackendRuntimeDeps(), Store: a.store, WorkspaceSelection: a.bindings.WorkspaceSelection,
		Settings: a.bindings.WorkspaceSettings, Planning: a.bindings.WorkspacePlanning, Workflow: a.bindings.WorkspaceWorkflow,
		Forms: a.bindings.Forms, FrontendID: a.FrontendID(), Effects: newEffectRunner(a.runtimeOwner),
		Feishu: a.Feishu(), Presentation: presentation,
	})
}
