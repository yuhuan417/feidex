package feishuapp

import (
	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	"feidex/internal/adapter/feishu/planmode"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	conversationapp "feidex/internal/application/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	"feidex/internal/runtime"
)

func commandRegistryForApp(a *App) *CommandRegistry {
	if a == nil || a.bindings == nil {
		return &CommandRegistry{}
	}
	configuredBackend := a.configView().configuredBackend
	makeSessionKey := a.configView().makeSessionKey
	registry := BuildCommandRegistry(CommandRegistryInputs{
		Features: BuildFeatureRegistryInputs(BuildCommandFeatureInputs(CommandFeatureDependencies{
			BindingScope: BindingScope{scope: a.bindings.BindingCommands.scope}, BindingCommands: a.bindings.BindingCommands,
			BackendSelection: a.bindings.BackendSelection, BackendConfiguration: a.bindings.BackendConfiguration,
			ReviewCommands: a.bindings.ReviewCommands, RuntimeSettings: a.bindings.RuntimeSettings,
			QuietMode: ConfiguredQuietModeBuilder(a.Config(), a.ConfigMu(), a.frontendConfigIndex),
			PlanMode:  newPlanModeAppAdapter(a), GoalCommands: a.bindings.GoalCommands,
			BackendActions: a.bindings.BackendActions, Compaction: a.bindings.Compaction, Download: a.bindings.Download,
			History: a.bindings.History, SkillCommands: a.bindings.SkillCommands, Usage: a.bindings.Usage,
			ThreadMenu: a.bindings.ThreadMenu, WorkspaceConfiguration: a.bindings.WorkspaceConfiguration,
			WorkspaceManagement: a.bindings.WorkspaceManagement, WorkspacePresentation: a.bindings.WorkspacePresentation,
			ModelCommands: a.bindings.ModelCommands, ModelSettings: a.bindings.ModelSettings,
			ScopedRoutingConfiguration: a.bindings.ScopedRoutingConfiguration, ServiceTier: a.bindings.ServiceTier,
			Debug: a.bindings.Debug, BackendUpgrades: a.bindings.BackendUpgrades,
			UpgradePresentation: a.bindings.UpgradePresentation, Upgrades: a.bindings.Upgrades,
			State: a.State(), Config: a.Config(), ConfiguredBackend: configuredBackend,
			MakeSessionKey: makeSessionKey, NormalizeSessionKey: a.configView().normalizeSessionKey,
			ConversationQuery: a.bindings.ConversationQuery, Conversations: a.bindings.Conversations,
			Renderer: a.Feishu(), Effects: newEffectRunner(a.runtimeOwner),
			FrontendID: a.FrontendID(), ReplyInThread: a.configView().replyInThreadEnabled(),
		})),
		ConfiguredBackend: configuredBackend, MakeSessionKey: makeSessionKey,
		WorkspaceConfigured:        func() bool { return a.Config() != nil && len(a.Config().Workspaces) > 0 },
		ReplyBackendSelection:      a.bindings.BackendSelection.ReplyBackendSelectionCard,
		BackendSwitchBlockedReason: a.runtimeOwner.BackendTransition.BackendSwitchBlockedReasonForTraffic,
		MaintenanceBlocksCommand:   CommandMaintenanceBlocker(a.BackendRuntimeDeps()),
		QueuePassthrough:           CommandPassthroughQueue(a.bindings.Submissions),
	})
	return &registry
}

func runFeishuAppConfigHeal(a *App) {
	if a == nil {
		return
	}
	runFeishuAppConfigHealWith(FeishuAppConfigHealInputs{
		Client: a.feishu, Config: a.cfg, ConfigMu: a.ConfigMu(), ConfigIndex: a.frontendConfigIndex,
		FrontendID: a.frontendID, State: a.stateView, Context: a.Context,
		Notifications: a.bindings.Notifications,
	})
}

func AttachEffectRunner(a *App, runner runtime.EffectRunner) {
	if a == nil || a.runtimeOwner == nil {
		return
	}
	a.runtimeOwner.EffectRunner = &runner
	if notifying, ok := a.feishu.(*appfeishuwrap.NotifyingFeishuClient); ok {
		a.feishu = &appfeishuwrap.EffectClient{
			NotifyingFeishuClient: notifying,
			Frontend:              identity.FrontendID(a.frontendID),
			Runner:                runner,
		}
	}
}

func HandleInboundCommand(a *App, msg *feishu.InboundMessage, raw string) error {
	return commandRegistryForApp(a).Handle(msg, raw)
}

func RenderMenuCommandFallback(a *App, actionName, sessionKey string) (map[string]any, bool) {
	return commandRegistryForApp(a).RenderFallback(actionName, sessionKey)
}

func findLocalCommandSpec(name string) *localCommandSpec {
	bindings := buildFeatureBindings(BuildFeatureRegistryInputs(CommandFeatureInputs{}))
	return findLocalCommandSpecIn(buildLocalCommandSpecs(bindings), name)
}

func newPlanModeAppAdapter(a *App) planmode.Dependencies {
	if a == nil {
		return planmode.Dependencies{}
	}
	return PlanModePorts(PlanModePortInputs{
		Runtime: a.BackendRuntimeDeps(), UseCase: a.bindings.Plan, Continuation: a.bindings.Continuation,
		State: a.State(), ModelSnapshots: a.bindings.ModelSnapshots,
		WorkspaceSelection: a.bindings.WorkspaceSelection, Submissions: a.bindings.Submissions,
		Conversations: a.bindings.Conversations, Feishu: a.feishu, AsyncRunner: a.asyncRunner,
	})
}

func sendCommandMenu(a *App, msg *feishu.InboundMessage) error {
	return sendCommandMenuWith(a.configView().makeSessionKey, a.configView().configuredBackend, a.State(), a.feishu, newEffectRunner(a.runtimeOwner), a.FrontendID(), a.configView().replyInThreadEnabled(), msg)
}

func commandWorkspace(a *App, msg *feishu.InboundMessage, args []string) error {
	return a.bindings.WorkspaceConfiguration.CommandWorkspace(msg, args, a.bindings.WorkspaceManagement)
}

func commandHelp(a *App, msg *feishu.InboundMessage, args []string) error {
	return handleHelpCommand(a.bindings.BindingCommands.scope, a.configView().configuredBackend, a.configView().makeSessionKey, a.State(), a.feishu, newEffectRunner(a.runtimeOwner), a.FrontendID(), a.configView().replyInThreadEnabled(), msg, args)
}

func commandQuiet(a *App, msg *feishu.InboundMessage, args []string) error {
	return handleQuietCommand(QuietCommandInputs{
		Settings: a.bindings.RuntimeSettings, Mode: ConfiguredQuietModeBuilder(a.Config(), a.ConfigMu(), a.frontendConfigIndex),
		MakeSessionKey: a.configView().makeSessionKey, State: a.State(), Renderer: a.feishu,
		Effects: newEffectRunner(a.runtimeOwner), FrontendID: a.FrontendID(), ReplyInThread: a.configView().replyInThreadEnabled(),
	}, msg, args)
}

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
