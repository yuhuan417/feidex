package feishuapp

import (
	"context"
	codexadapter "feidex/internal/adapter/backend/codex"
	configadapter "feidex/internal/adapter/config"
	"feidex/internal/adapter/feishu/approval"
	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/finalcardpatch"
	"feidex/internal/adapter/feishu/goalcmd"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/adapter/feishu/turnmeta"
	"feidex/internal/adapter/feishu/turnstream"
	"feidex/internal/adapter/feishu/upgraderender"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	filesystempicker "feidex/internal/adapter/filesystem/pathpicker"
	statejson "feidex/internal/adapter/storage/json"
	scoped "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application"
	"feidex/internal/application/announcement"
	"feidex/internal/application/asyncinput"
	retry "feidex/internal/application/autoretry"
	"feidex/internal/application/backendevents"
	"feidex/internal/application/backendfailure"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/application/backendselection"
	"feidex/internal/application/cardaction"
	"feidex/internal/application/compaction"
	"feidex/internal/application/continuation"
	"feidex/internal/application/conversation"
	"feidex/internal/application/fileshare"
	frontendapp "feidex/internal/application/frontend"
	"feidex/internal/application/goal"
	"feidex/internal/application/inbound"
	"feidex/internal/application/interaction"
	"feidex/internal/application/modelconfig"
	"feidex/internal/application/pathpicker"
	planapp "feidex/internal/application/plan"
	reviewapp "feidex/internal/application/review"
	"feidex/internal/application/routing"
	"feidex/internal/application/runtimeconfig"
	"feidex/internal/application/submission"
	"feidex/internal/application/threadsettings"
	"feidex/internal/application/turn"
	"feidex/internal/application/upgrade"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/compositionkit"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	"feidex/internal/runtime"
	clauderuntime "feidex/internal/runtime/claude"
	codexruntime "feidex/internal/runtime/codex"
	"feidex/internal/runtime/maintenance"
	"feidex/internal/runtime/turnbinding"
	upgradeunits "feidex/internal/runtime/upgrade"
	runtimeworkspace "feidex/internal/runtime/workspace"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func testWorkspacePresentation(a *App) *workspacecards.Presentation {
	if a == nil {
		return compositionkit.NewWorkspacePresentation(compositionkit.WorkspacePresentationDependencies{})
	}
	return compositionkit.NewWorkspacePresentation(compositionkit.WorkspacePresentationDependencies{
		Frontend: identity.FrontendID(a.FrontendID()), Config: a.Config(), ConfigPath: a.ConfigPath(),
		Mutex: a.ConfigMu(), Scopes: a.State(),
		Backend: ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex()),
	})
}

func testStateView(a *App) *scoped.Store {
	if a == nil {
		return nil
	}
	view := scoped.NewScoped(a.store, a.FrontendID(), a.configView().configuredBackend())
	view.RevisionMutex = a.ConfigMu()
	return view
}

// Focused fixtures explicitly construct their complete dependency graph.
func prepareTestApp(a *App) *App {
	if a.runtimeOwner == nil {
		a.runtimeOwner = runtime.NewFrontendOwner()
	}
	if a.transport == nil {
		a.transport = a.feishu
	}
	if a.stateView == nil {
		a.stateView = testStateView(a)
	}
	if a.runtimeOwner.TurnBindings == nil {
		a.runtimeOwner.TurnBindings = turnbinding.NewTracker(a.State().Submission)
	}
	a.bindings = &Bindings{
		TurnStreams: turnstream.NewTracker(), TurnItems: turnitem.NewTracker(), FinalCardPatches: finalcardpatch.NewTracker(), Goals: goal.NewTracker(),
		Submissions: &submission.SubmissionQueueService{}, PendingQueue: &submission.PendingQueueService{},
		Continuation: &continuation.Service{}, Turns: &turn.Service{}, TurnPresentation: &turnstream.Service{},
		Compaction: &compaction.Service{}, GoalContinuation: &goal.Service{}, GoalCommands: &goalcmd.Service{},
		Interactions: &interaction.Service{}, BackendEvents: &backendevents.Service{}, Plan: &planapp.Service{},
	}
	if a.runtimeOwner.EffectRunner == nil {
		runner := NewEffectRunner(testEffectRunnerInputs(a))
		a.runtimeOwner.EffectRunner = &runner
	}
	a.bindings.RuntimeSettings = runtimeconfig.Service{Repository: configadapter.NewRuntimeRepository(a)}
	a.bindings.PathPicker = pathpicker.Service{Filesystem: filesystempicker.Filesystem{}}
	a.bindings.AsyncInputs = asyncinput.Service{Deps: asyncinput.Dependencies{Repository: a.State(), Backend: func() string { return a.configView().configuredBackend() }, Context: a.Context, Run: SessionTaskRunner(a.runtimeOwner.SessionActors, func(fn func()) bool {
		return a.runtimeOwner.Lifecycle.Run(fn, a.asyncRunner)
	}), Effects: newEffectRunner(a.runtimeOwner)}}
	a.bindings.WorkspaceSelection = workspaceapp.SelectionService{Frontend: identity.FrontendID(a.FrontendID()), Repository: scoped.WorkspaceSelections{Store: a.State()}, DefaultWorkspaceID: DefaultWorkspaceID(a.Config(), a.ConfigMu())}
	a.bindings.GoalManagement = &goal.Management{Tracker: a.bindings.Goals, Context: a.Context, Gateway: func() (goal.Gateway, error) { return RequireCodexGoalGateway(a.runtimeView().getCodex()) }}
	a.bindings.SubmissionLookup = submission.SubmissionLookupService{State: a.State(), Runtime: a.runtimeOwner.TurnBindings}
	a.bindings.SubmissionStatus = submission.StatusService{Lookup: a.bindings.SubmissionLookup, Repository: a.State()}
	a.bindings.InteractionLifecycle = interaction.LifecycleService{Repository: a.State(), Frontend: a.FrontendID(), Presentation: InteractionExpiryPresentation(a.Feishu(), a.State(), identity.FrontendID(a.FrontendID()), *a.runtimeOwner.EffectRunner)}
	a.bindings.ModelAcknowledgements = modelconfig.AcknowledgementService{Repository: a.State()}
	a.bindings.ClaudeFactory = func(cfg config.ClaudeConfig) ClaudeCore {
		return clauderuntime.NewService(testClaudeRuntimePorts(a, cfg))
	}
	routingConfiguration := routing.ConfigurationService{Repository: a.State(), Frontend: identity.FrontendID(a.FrontendID())}
	a.bindings.RoutingConfiguration = compositionkit.RoutingConfiguration{ConfigurationService: routingConfiguration, Runner: newEffectRunner(a.runtimeOwner), Context: a.Context()}
	a.bindings.ScopedRoutingConfiguration = compositionkit.ScopedRoutingConfiguration{Service: routing.ScopedConfigurationService{ConfigurationService: routingConfiguration, BackendSource: func() string { return a.configView().configuredBackend() }}, Runner: newEffectRunner(a.runtimeOwner), Context: a.Context()}
	primaryRepository := statejson.NewGroupPrimaryRepository(a.store, a.FrontendID())
	a.bindings.Primary = routing.Service{Repository: primaryRepository}
	feishuClient := a.feishu
	a.bindings.GroupMessages = routing.GroupMessages{Frontend: a.FrontendID(), Primary: a.bindings.Primary, Links: a.State(), SelfOpenID: LiveBotOpenID(feishuClient)}
	a.bindings.Announcements = announcement.Service{Repository: a.State(), Gateway: AnnouncementGateway(a.feishu), Frontend: a.FrontendID(), Primary: func(chatID string) bool {
		enabled, _ := a.bindings.Primary.IsPrimary(a.FrontendID(), "group", chatID)
		return enabled
	}}
	a.bindings.AnnouncementQuery = announcement.Query{Repository: a.State(), Workspaces: configadapter.NewWorkspaceRepositoryForConfig(a.Config(), a.ConfigMu(), a.ConfigPath()), HasPrimary: func(chatID string) bool {
		record, _ := a.bindings.Primary.Lookup(a.FrontendID(), "group", chatID)
		return record != nil
	}}
	a.bindings.ConversationQuery = conversation.Query{Repository: a.State()}
	if a.runtimeOwner.Announcements == nil {
		a.runtimeOwner.Announcements = runtime.NewCoalescedRefresh(&a.runtimeOwner.Lifecycle, 2*time.Second, 15*time.Second, GroupAnnouncementRefresh(GroupAnnouncementRefreshDependencies{
			FrontendID: a.FrontendID(), Feishu: a.Feishu(), Config: a.Config(), ConfigMu: a.ConfigMu(),
			FrontendConfigIndex: a.FrontendConfigIndex(), RuntimeOwner: a.runtimeOwner,
			Announcements: a.bindings.Announcements, AnnouncementQuery: a.bindings.AnnouncementQuery, ConversationQuery: a.bindings.ConversationQuery,
		}))
	}
	liveThreads := SubmissionLiveThreads(a.runtimeOwner.LiveThreads, a.State().Session, a.bindings.AnnouncementQuery, a.runtimeOwner.Announcements.Schedule)
	a.bindings.PrimaryInitialization = routing.InitializationService{Repository: primaryRepository, BotCount: func(ctx context.Context, chatID string) (int, error) {
		return feishuClient.GetGroupBotCount(ctx, chatID)
	}, LiveBotOpenID: LiveBotOpenID(feishuClient)}
	a.bindings.TurnMetadata = turnmeta.Service{Tracker: a.runtimeOwner.TurnBindings}
	a.bindings.ItemContext = approval.ItemContext{Items: a.bindings.TurnItems, Started: func(threadID, turnID string) { a.bindings.Turns.BindPendingSubmissionTurn(threadID, turnID, true) }}
	a.bindings.PendingReplies = PendingReplyAdapter{Service: a.bindings.Interactions, Repository: a.State()}
	filesystem, git := WorkspaceCreationPorts(a.ConfigPath())
	workspaceLifecycle := &workspaceapp.Lifecycle{Frontend: identity.FrontendID(a.FrontendID()), Selection: a.bindings.WorkspaceSelection, Configuration: workspaceapp.ConfigurationService{Repository: configadapter.NewWorkspaceRepository(a)}, Repository: configadapter.WorkspaceLifecycleRepository{Source: a, Scope: a.State()}}
	a.bindings.WorkspaceCreation = &workspaceapp.CreationService{Filesystem: filesystem, Git: git, Lifecycle: workspaceLifecycle}
	a.bindings.WorkspaceSettings = workspaceapp.SettingsService{Frontend: a.FrontendID(), Repository: configadapter.WorkspaceSettingsRepository{Source: a, Scope: a.State()}}
	a.bindings.WorkspacePlanning = &workspaceapp.PlanningService{Repository: configadapter.NewWorkspaceRepository(a), PendingRequests: a.State().PendingRequests, ConfigPath: a.ConfigPath, BotName: func() string { return currentBotDisplayName(feishuClient) }, FrontendID: a.FrontendID, Paths: runtimeworkspace.PlanningFilesystem{}, Git: runtimeworkspace.PlanningGit{}}
	a.bindings.Forms = &interaction.FormService{Repository: a.State()}
	a.bindings.WorkspaceWorkflow = &workspaceapp.Workflow{Forms: a.bindings.Forms, Planning: a.bindings.WorkspacePlanning, Creation: a.bindings.WorkspaceCreation}
	a.bindings.WorkspacePresentation = testWorkspacePresentation(a)
	platform, releases, artifacts, launcher := UpgradeWorkflowPorts(a.Config(), a.ConfigMu(), a.runtimeOwner)
	a.bindings.UpgradeWorkflow = &upgrade.Service{Forms: a.bindings.Forms, Platform: platform, Releases: releases, Artifacts: artifacts, Launcher: launcher}

	a.bindings.Maintenance = backendmaintenance.NewMaintenanceStateService(
		a.runtimeOwner.MaintenanceTrackers,
		MaintenanceRepository(a.State(), a.FrontendID()),
	)
	a.bindings.StartupState = conversation.StartupState{Repository: a.State(), DefaultWorkspaceID: func() string { return a.bindings.WorkspaceSelection.ResolveSession(nil) }}
	a.bindings.UpgradePoller = upgrade.Poller{Repository: a.State(), Units: upgradeunits.Units{}}
	a.bindings.Notifications = frontendapp.Notifications{Repository: a.State(), Sender: NotificationSender(a.feishu, a.FrontendID(), *a.runtimeOwner.EffectRunner), Context: a.Context}
	a.bindings.MaintenanceCommands = BuildMaintenanceCommands(MaintenanceCommandInputs{
		Context: a.Context, Repository: a.State(), Poller: a.bindings.UpgradePoller,
		Feishu: a.Feishu(), FrontendID: a.FrontendID(), EffectRunner: *a.runtimeOwner.EffectRunner,
		Config: a.Config(), QueueNotification: a.bindings.Notifications.Queue, ReadyChatIDs: maintenance.StartupReadyChatIDs,
		RunAsync: func(fn func()) { a.runtimeOwner.Lifecycle.Run(fn, a.AsyncRunner()) },
	})
	a.bindings.SubmissionCleanup = maintenance.SubmissionCleanup{Repository: a.State(), Runtime: a.runtimeOwner.TurnBindings, Items: a.bindings.TurnItems}
	configuredBackend := ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex())
	a.bindings.AutoRetry = AutoRetryView(AutoRetryViewInputs{
		Context: a.Context, Config: a.Config(), ConfigMu: a.ConfigMu(), ConfiguredBackend: configuredBackend,
		FrontendID: a.FrontendID(), FrontendConfigIndex: a.FrontendConfigIndex(), BackendDriver: appbackend.SelectedDriver{Selected: configuredBackend},
		EffectRunner: *a.runtimeOwner.EffectRunner, Feishu: a.Feishu(),
	})
	autoRetryRuntimeDeps := &BackendRuntimeDeps{}
	a.bindings.AutoRetry.Engine = retry.NewEngine(AutoRetryPorts(AutoRetryPortInputs{
		Context: a.Context, Tracker: a.runtimeOwner.AutoRetries, Repository: a.State(), Live: liveThreads,
		Enabled: func() bool { return a.bindings.AutoRetry.Settings().Enabled }, SaveEnabled: a.bindings.RuntimeSettings.SetAutoRetry,
		RuntimeDeps: autoRetryRuntimeDeps, RuntimeOwner: a.runtimeOwner,
		RunAsync:   func(fn func()) { a.runtimeOwner.Lifecycle.Run(fn, a.AsyncRunner()) },
		FrontendID: a.FrontendID(), Config: a.Config(), ConfigMu: a.ConfigMu(),
		Starter: a.bindings.Submissions, Presenter: a.bindings.AutoRetry,
	}))
	a.bindings.FrontendQuery = frontendapp.Query{Repository: a.State(), Facts: FrontendFacts(a.runtimeOwner, a.bindings.Maintenance), Retrying: a.bindings.AutoRetry.HasBlockingAutoRetry}
	var codexUpgrade codexruntime.UpgradeService
	var backendFailureOwner *backendfailure.BackendFailureService
	var recoverFrontendRuntimeState func() error
	recoverFrontend := func() {
		if recoverFrontendRuntimeState != nil {
			_ = recoverFrontendRuntimeState()
		}
	}
	a.bindings.CodexRecovery = codexruntime.NewRecoveryService(CodexRecoveryPorts(CodexRecoveryPortInputs{
		Runtime: a.BackendRuntimeDeps(), Submissions: a.bindings.Submissions, AsyncRunner: a.AsyncRunner(),
		BackendFailure: func() *backendfailure.BackendFailureService { return backendFailureOwner },
		StartVerifiedCodexClient: func(ctx context.Context) (codexruntime.CodexClient, error) {
			return codexUpgrade.StartVerifiedCodexClient(ctx)
		},
		RecoverFrontendRuntime: recoverFrontend,
	}))
	codexUpgrade = codexruntime.NewUpgradeService(CodexUpgradePorts(
		a.Config(), a.ConfigMu(), a.FrontendID(), a.FrontendConfigIndex(),
		a.runtimeOwner, a.BackendRuntimeDeps(), a.bindings.CodexRecovery,
		recoverFrontend,
		func(ctx context.Context) error { return codexUpgrade.CodexSmokeTest(ctx) },
	))
	a.bindings.CodexUpgrade = codexUpgrade
	smoke, active, current, create := ClaudeMaintenancePorts(a.Config(), a.ConfigMu(), a.Context, a.runtimeOwner, a.FrontendConfigIndex(), a.bindings.ClaudeFactory)
	a.bindings.ClaudeMaintenance = &clauderuntime.Maintenance{Smoke: smoke, Active: active, Current: current, Create: create}
	a.bindings.BackendMaintenance = make(map[string]*backendmaintenance.Service)
	a.bindings.MaintenanceRunners = make(map[string]maintenance.OperationRunner)
	maintenanceRenderer := a.feishu
	maintenanceFrontend := identity.FrontendID(a.FrontendID())
	maintenanceEffects := newEffectRunner(a.runtimeOwner)
	for kind, name := range map[string]string{"codex": "Codex", "claude": "Claude"} {
		spec := upgraderender.CodexSpec
		if kind == "claude" {
			spec = upgraderender.ClaudeSpec
		}
		renderUpgrade := func(sessionKey string, snapshot appbackend.BackendUpgradeSnapshot) map[string]any {
			return upgraderender.RenderUpgradeOperationCard(spec, maintenanceRenderer, sessionKey, snapshot)
		}
		renderRestart := func(sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
			return upgraderender.RenderRestartOperationCard(spec, maintenanceRenderer, sessionKey, snapshot)
		}
		patchCard := func(ctx context.Context, messageID string, card map[string]any) error {
			return maintenanceEffects.Run(ctx, []application.Effect{application.PatchCard{
				Frontend: maintenanceFrontend, MessageID: messageID,
				View: feishuoutbound.Card(card),
			}})
		}
		installer, busy, backendRuntime, publisher := BackendMaintenancePorts(BackendMaintenancePortValues{
			Config: a.Config(), ConfigMu: a.ConfigMu(), Kind: kind,
			Frontend: identity.FrontendID(a.FrontendID()), State: a.bindings.Maintenance,
			CodexUpgrade: a.bindings.CodexUpgrade, ClaudeMaintenance: a.bindings.ClaudeMaintenance,
			RenderUpgrade: renderUpgrade, RenderRestart: renderRestart, PatchCard: patchCard,
		})
		service := &backendmaintenance.Service{Name: name, Kind: kind, Forms: a.bindings.Forms, Installer: installer, State: a.runtimeOwner.MaintenanceTrackers.Get(runtime.BackendKey(kind)), BusyReason: busy, Runtime: backendRuntime, Publisher: publisher}
		a.bindings.BackendMaintenance[kind] = service
		a.bindings.MaintenanceRunners[kind] = maintenance.OperationRunner{Lifecycle: &a.runtimeOwner.Lifecycle, Service: service, Executor: a.asyncRunner}
	}
	a.bindings.UpgradePresentation = BuildUpgradePresentation(maintenanceRenderer, a.bindings.BackendMaintenance)
	a.bindings.BackendUpgrades = BuildBackendUpgrades(BackendUpgradeInputs{
		SessionKey: SessionKeyBuilder(a.FrontendID()), ReplyInThread: func() bool { return false },
		FrontendID: identity.FrontendID(a.FrontendID()), Runner: *a.runtimeOwner.EffectRunner,
		Lifecycle: &a.runtimeOwner.Lifecycle, AsyncRunner: a.asyncRunner,
		BackendMaintenance: a.bindings.BackendMaintenance, MaintenanceRunners: a.bindings.MaintenanceRunners,
		Maintenance: a.bindings.Maintenance, Presentation: a.bindings.UpgradePresentation,
		Forms: a.bindings.Forms, CodexUpgrade: a.bindings.CodexUpgrade,
	})
	a.bindings.History = BuildHistory(
		identity.FrontendID(a.FrontendID()), a.State(), ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex()),
		func() codexadapter.RPCClient { return a.runtimeView().currentCodexClient() },
		a.runtimeOwner.Lifecycle.Context, *a.runtimeOwner.EffectRunner,
		SessionKeyBuilder(a.FrontendID()), func(string) bool { return false },
	)
	debugViewDependencies := DebugViewDependencies(a)
	sharedArtifacts, downloadPresentation, downloadRunner := FileSharePorts(a.feishu, debugViewDependencies, &a.runtimeOwner.Lifecycle, a.runtimeOwner.SessionActors, a.asyncRunner)
	a.bindings.FileSharing = &fileshare.Service{Forms: a.bindings.Forms, Repository: a.State(), Artifacts: sharedArtifacts, Presentation: downloadPresentation, Context: a.Context, Run: downloadRunner}
	debugViewDependencies.FileSharing = a.bindings.FileSharing
	a.bindings.Debug = BuildDebug(debugViewDependencies)
	a.bindings.Usage = BuildUsage(debugViewDependencies)
	a.bindings.FinalCardPatch = BuildFinalCardPatch(FinalCardPatchInputs{
		Context: a.Context, Tracker: a.bindings.FinalCardPatches, Finder: a.State(),
		Patcher: a.Feishu(), RunAsync: a.AsyncRunner(), Config: a.Config(), State: a.State(),
	})
	a.bindings.ClaudeSupport = BuildClaudeSupport(a)
	a.bindings.ThreadSettings = threadsettings.Service{Repository: a.State()}
	permissionBackend := ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex())
	permissionMenuRenderer := ClaudePermissionMenuRenderer(a.Config(), permissionBackend, a.State().Session)
	a.bindings.PermissionSettings = threadsettings.PermissionService{
		Settings: a.bindings.ThreadSettings,
		Source:   configadapter.ThreadPermissionRepository{Source: a, Scope: a.State()},
		Runtime:  PermissionRuntime(permissionBackend, a.runtimeOwner.ClaudeCore),
		Tasks:    PermissionTasks(&a.runtimeOwner.Lifecycle, a.runtimeOwner.SessionActors, a.AsyncRunner()),
		Failure:  PermissionFailure(permissionMenuRenderer, a.FrontendID(), *a.runtimeOwner.EffectRunner),
		Context:  a.Context,
	}
	a.bindings.ServiceTier = BuildServiceTier(
		a.bindings.ThreadSettings, a.Context, identity.FrontendID(a.FrontendID()),
		*a.runtimeOwner.EffectRunner, SessionKeyBuilder(a.FrontendID()),
	)
	a.bindings.ModelSnapshots = modelconfig.SnapshotService{Repository: ModelSnapshotRepository(a.Config(), a.ConfigMu(), a.State())}
	a.bindings.ModelSettings = modelconfig.SettingsService{Repository: a.State(), Admission: ModelWriteAdmission(a.bindings.FrontendQuery), Frontend: identity.FrontendID(a.FrontendID())}
	a.bindings.ModelDefaults = modelconfig.DefaultsService{
		Repository: configadapter.ModelDefaultsRepository{Source: a, Scope: a.State()},
		Admission:  ModelWriteAdmission(a.bindings.FrontendQuery), Frontend: a.FrontendID(),
		Publisher: ModelDefaultsPublisher(a.runtimeOwner, a.Config(), a.ConfigMu()),
	}
	a.bindings.ModelOptions = modelconfig.OptionsService{Repository: configadapter.ModelOptionsRepository{Source: a}}
	a.bindings.ModelCommands = BuildModelCommands(a)
	a.bindings.ConversationConfiguration = conversation.Configuration{Models: a.bindings.ModelSnapshots, ServiceName: CodexServiceName(a.Config(), a.ConfigMu())}
	a.bindings.TurnStarter = submission.TurnStarter{Frontend: identity.FrontendID(a.FrontendID()), Effects: newEffectRunner(a.runtimeOwner), Collaboration: a.bindings.Plan}
	a.bindings.BindingPending = routing.PendingService{Configuration: a.bindings.RoutingConfiguration.ConfigurationService, Repository: a.State()}
	a.bindings.BackendConfiguration = BuildBackendConfiguration(BackendConfigurationInputs{
		Config: a.Config(), ConfigMu: a.ConfigMu(), Backend: a.runtimeOwner.Backend,
		FrontendConfigIndex: a.FrontendConfigIndex(), Store: a.store,
		WorkspaceSelection: a.bindings.WorkspaceSelection, Driver: appbackend.SelectedDriver{Selected: configuredBackend}, ModelCommands: a.bindings.ModelCommands,
	})
	a.bindings.BackendActions = BuildBackendActions(a)
	a.bindings.ServerRequests = BuildServerRequests(a)
	a.bindings.Skills = compositionkit.NewSkillService(SkillUseCasePorts(a.Config(), a.ConfigMu(), a.Context, a.State(), a.runtimeOwner.PendingSkills, a.FrontendID(), a.runtimeOwner))
	a.bindings.SkillCommands = BuildSkillCommands(SkillCommandInputs{
		Service: a.bindings.Skills, FrontendID: a.FrontendID(), EffectRunner: *a.runtimeOwner.EffectRunner,
		Actors:   a.runtimeOwner.SessionActors,
		RunAsync: func(fn func()) bool { return a.runtimeOwner.Lifecycle.Run(fn, a.asyncRunner) },
	})
	*a.bindings.PendingQueue = submission.NewPendingQueueService(PendingQueuePorts(a.Context, a.State(), a.bindings.SubmissionCleanup, a.Config(), a.ConfigMu(), a.Feishu()))
	a.bindings.Continuation.Deps = ContinuationPorts(a.Config(), a.ConfigMu(), a.Context, a.State(), a.runtimeOwner, a.bindings.Submissions, a.FrontendID(), a.FrontendConfigIndex(), a.Feishu())
	a.bindings.Compaction.Deps = CompactionPorts(a.Context, a.State(), a.runtimeOwner, a.FrontendID(), a.feishu != nil)
	a.bindings.GoalContinuation.Deps = goal.Dependencies{
		Context: a.Context, Repository: a.State(), Tracker: a.bindings.Goals,
		Presenter: GoalContinuationPresenter(GoalCommandOutbound(identity.FrontendID(a.FrontendID()), newEffectRunner(a.runtimeOwner))),
		Bindings:  a.runtimeOwner.TurnBindings, Replies: a.bindings.Continuation, Streams: a.bindings.TurnPresentation,
		Live:               GoalContinuationLiveThreads(a.runtimeOwner.LiveThreads, a.State(), a.bindings.AnnouncementQuery, a.runtimeOwner.Announcements),
		DefaultWorkspaceID: DefaultWorkspaceID(a.Config(), a.ConfigMu()),
		BelongsToFrontend:  func(key string) bool { return FrontendSessionBelongsToFrontend(a.FrontendID(), key) },
	}
	*a.bindings.GoalCommands = goalcmd.NewService(goalcmd.Dependencies{
		StateProvider:  a.State(),
		Outbound:       GoalCommandOutbound(identity.FrontendID(a.FrontendID()), newEffectRunner(a.runtimeOwner)),
		CardRenderer:   a.feishu,
		GoalManagement: a.bindings.GoalManagement,
		GoalTracker:    a.bindings.Goals,
		MakeSessionKeyFn: func(msg *feishu.InboundMessage) string {
			return GoalCommandSessionKey(a.FrontendID(), msg)
		},
		ReplyInThreadEnabledFn: func(string) bool { return false },
		MenuCardBodyForSessionFn: func(_, action, body string) string {
			return menuCardBody(action, body)
		},
		ActionStringValueFn: GoalCommandActionStringValue,
		ActionSessionKeyFn:  GoalCommandActionSessionKey,
		CompleteMenuCommandFn: func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
			return CompleteGoalMenuCommand(a, action, sessionKey, rawCommand, parentAction)
		},
		ContextFn: a.runtimeOwner.Lifecycle.Context,
	})
	a.bindings.Interactions.Deps = InteractionPorts(a.State(), a.bindings.SubmissionLookup)
	a.bindings.InteractionDelivery = &interaction.DeliveryService{Repository: a.State()}
	reviewCards := NewOutboundCardService(OutboundCardInputs{
		RuntimeDeps: a.BackendRuntimeDeps(), Feishu: a.Feishu(), AsyncRunner: a.AsyncRunner(),
		InteractionDelivery: a.bindings.InteractionDelivery, TurnPresentation: a.bindings.TurnPresentation,
		Continuation: a.bindings.Continuation, FinalCardPatch: a.bindings.FinalCardPatch,
		TurnFinalFooter: a.bindings.TurnMetadata.TurnFinalFooterLines,
	})
	a.bindings.OutboundCards = reviewCards
	review := reviewapp.NewService(ReviewPorts(ReviewPortInputs{
		Runtime: a.BackendRuntimeDeps(), Forms: a.bindings.Forms, Delivery: a.bindings.InteractionDelivery,
		Context: a.Context, Repository: a.State(), Submissions: a.bindings.Submissions,
		PendingQueue: a.bindings.PendingQueue, Cards: reviewCards,
	}))
	a.bindings.Review = &review
	queuedNotice, expirePlan := SubmissionNoticePorts(SubmissionNoticeInputs{
		Config: a.Config(), ConfigMu: a.ConfigMu(), FrontendID: a.FrontendID(),
		FrontendConfigIndex: a.FrontendConfigIndex(), State: a.State(), RuntimeOwner: a.runtimeOwner,
		Continuation: a.bindings.Continuation, Feishu: a.feishu, Runner: *a.runtimeOwner.EffectRunner,
	})
	*a.bindings.Submissions = submission.NewSubmissionQueueService(SubmissionPorts(SubmissionPortInputs{
		Plan: a.bindings.Plan, TurnPresentation: a.bindings.TurnPresentation, LiveThreads: liveThreads,
		Config: a.Config(), ConfigMu: a.ConfigMu(), FrontendID: a.FrontendID(),
		FrontendConfigIndex: a.FrontendConfigIndex(), State: a.State(), Context: a.Context,
		Feishu: a.feishu, RuntimeOwner: a.runtimeOwner, RuntimeDeps: a.BackendRuntimeDeps(),
		AsyncRunner: a.AsyncRunner(), PendingQueue: a.bindings.Continuation, SkillResolver: a.bindings.Skills,
		Continuation: a.bindings.Continuation, TurnItems: a.bindings.TurnItems, RuntimeMaintenance: a.bindings.SubmissionCleanup,
		AutoRetry: a.bindings.AutoRetry, WorkspaceSelection: a.bindings.WorkspaceSelection,
		ModelSettings: a.bindings.ModelSnapshots, ConversationConfiguration: a.bindings.ConversationConfiguration,
		Starts: a.runtimeOwner.SubmissionStarts, QueuedNotice: queuedNotice, ExpirePlan: expirePlan,
		ReplyText:       SubmissionReplyTextPort(a.FrontendID(), *a.runtimeOwner.EffectRunner),
		MarkQueued:      a.bindings.PendingQueue.MarkSubmissionQueuedReactions,
		MarkRunning:     a.bindings.PendingQueue.MarkSubmissionRunningReactions,
		ClearProcessing: a.bindings.PendingQueue.ClearSubmissionProcessingReactions,
		StartTurn:       a.bindings.TurnStarter.Start, StartReview: a.bindings.Review.StartSubmission,
	}))
	turnPlanMode := &planmode.Dependencies{}
	runtimeDeps := a.BackendRuntimeDeps()
	*a.bindings.Turns = turn.NewService(TurnPorts(TurnPortInputs{
		Runtime: runtimeDeps, TurnPresentation: a.bindings.TurnPresentation, Cards: a.bindings.OutboundCards,
		TurnMetadata: a.bindings.TurnMetadata, Continuation: a.bindings.Continuation,
		PendingQueue: a.bindings.PendingQueue, Submissions: a.bindings.Submissions, AutoRetry: a.bindings.AutoRetry,
		SubmissionCleanup: a.bindings.SubmissionCleanup, Compaction: a.bindings.Compaction,
		GoalContinuation: a.bindings.GoalContinuation, PlanMode: turnPlanMode,
		AnnouncementQuery: a.bindings.AnnouncementQuery, AsyncRunner: a.AsyncRunner(),
	}))
	*a.bindings.TurnPresentation = turnstream.NewService(TurnPresentationPorts(TurnPresentationPortInputs{
		Runtime: runtimeDeps, Turns: a.bindings.Turns, TurnPresentation: a.bindings.TurnPresentation,
		Tracker: a.bindings.TurnStreams, Finder: a.bindings.SubmissionLookup, Items: a.bindings.TurnItems,
		Compaction: a.bindings.Compaction, SubmissionStatus: a.bindings.SubmissionStatus, Cards: a.bindings.OutboundCards,
	}))
	a.bindings.TurnReconciliation = turn.Reconciliation{Gateway: TurnReconciliationGateway(a.BackendRuntimeDeps()), Session: a.State().Session, SawFinal: a.bindings.TurnPresentation.StreamSawFinal, Finish: a.bindings.Turns.FinishTurn, Context: a.Context}
	a.bindings.ClaudeReconciliation = turn.StoppedReconciliation{Stopped: ClaudeSessionStopped(ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex()), a.runtimeOwner.ClaudeCore), Session: a.State().Session, Finish: a.bindings.Turns.FinishTurn}
	workspaceRepository := configadapter.NewWorkspaceRepository(a)
	a.bindings.BackendEvents.Deps = backendevents.Dependencies{
		Lifecycle: a.bindings.Turns, Items: a.bindings.TurnItems, Presentation: a.bindings.TurnPresentation,
		Compaction: a.bindings.Compaction, Submissions: a.bindings.SubmissionStatus,
		Usage: a.runtimeOwner.TurnBindings, Goals: a.bindings.Goals, Interactions: a.bindings.Interactions,
		InteractionPresenter: BackendInteractionPresenter(BackendInteractionPresenterPorts{
			FindSubmissionByTurn:      a.bindings.SubmissionLookup.FindSubmissionByTurn,
			MergeApprovalPresentation: a.bindings.ItemContext.MergePresentation,
			WorkspaceCwd: func(workspaceID string) string {
				workspace, err := workspaceRepository.Get(workspaceID)
				if err != nil || workspace == nil {
					return ""
				}
				return workspace.Cwd
			},
			SendApprovalCardPresentation: a.bindings.ServerRequests.SendApprovalCardPresentation,
			SendUserInputCard:            a.bindings.ServerRequests.SendUserInputCard,
			SendUserInputFormCard:        a.bindings.ServerRequests.SendUserInputFormCard,
			SendElicitationURLCard:       a.bindings.ServerRequests.SendElicitationURLCard,
			SendElicitationFormCard:      a.bindings.ServerRequests.SendElicitationFormCard,
			ReplyCodexError:              CodexErrorReplyPort(a.runtimeOwner),
		}),
	}
	failureCards := NewOutboundCardService(OutboundCardInputs{
		RuntimeDeps: runtimeDeps, Feishu: a.Feishu(), AsyncRunner: a.AsyncRunner(),
		InteractionDelivery: a.bindings.InteractionDelivery, TurnPresentation: a.bindings.TurnPresentation,
		Continuation: a.bindings.Continuation, FinalCardPatch: a.bindings.FinalCardPatch,
		TurnFinalFooter: a.bindings.TurnMetadata.TurnFinalFooterLines,
	})
	failure := backendfailure.NewBackendFailureService(BackendFailurePorts(BackendFailurePortInputs{
		Runtime: runtimeDeps, TurnPresentation: a.bindings.TurnPresentation, Compaction: a.bindings.Compaction,
		InteractionLifecycle: a.bindings.InteractionLifecycle, AutoRetry: a.bindings.AutoRetry,
		SubmissionCleanup: a.bindings.SubmissionCleanup, PendingQueue: a.bindings.PendingQueue,
		Submissions: a.bindings.Submissions, Cards: failureCards, AsyncRunner: a.AsyncRunner(),
	}))
	a.bindings.BackendFailure = &failure
	backendFailureOwner = a.bindings.BackendFailure
	inboundService := &inbound.Service{}
	forwardService := inbound.ForwardService{Gateway: ForwardGateway(a.feishu), Tasks: ForwardTasks(&a.runtimeOwner.Lifecycle, a.asyncRunner), Context: a.Context, Process: ForwardProcessor(a.runtimeOwner.SessionActors, SessionKeyBuilder(a.FrontendID()), func(msg *application.InboundMessage) error { return inboundService.ProcessMessage(msg) }), Queued: a.bindings.PendingQueue.MarkMessagesQueuedReactions, Clear: a.bindings.PendingQueue.ClearMessageProcessingReactions, Failed: ForwardFailure(a.runtimeOwner.Lifecycle.Context, a.FrontendID(), *a.runtimeOwner.EffectRunner)}
	a.bindings.ForwardInputs = forwardService
	a.bindings.Conversations = &conversation.Service{Deps: ConversationPorts(ConversationPortInputs{
		Config: a.Config(), ConfigMu: a.ConfigMu(), FrontendID: a.FrontendID(),
		FrontendConfigIndex: a.FrontendConfigIndex(), Context: a.Context,
		Repository: a.State(), RuntimeOwner: a.runtimeOwner, LiveThreads: liveThreads,
		ModelSettings: a.bindings.ModelSnapshots,
		ThreadBinding: conversation.ThreadBindingDependencies{
			Lookup: a.bindings.SubmissionLookup, Bindings: a.runtimeOwner.TurnBindings, Replies: a.bindings.Continuation,
		},
		ConversationConfiguration: a.bindings.ConversationConfiguration,
		ContinueClaude:            a.bindings.Continuation.ContinueClaudeSessionWithText,
	})}
	a.bindings.WorkspaceConfiguration = BuildWorkspaceConfiguration(a, a.bindings.WorkspacePresentation, a.bindings.Conversations)
	bindingScope := NewBindingScope(a.State(), a.configView().normalizeSessionKey, a.bindings.Primary, a.FrontendID())
	a.bindings.WorkspaceManagement = BuildWorkspaceManagement(a, a.bindings.WorkspacePresentation, a.bindings.Conversations, bindingScope)
	a.bindings.Upgrades = BuildUpgrades(UpgradeInputs{
		Context: a.Context, Config: a.Config(), ConfigMu: a.ConfigMu(), ConfiguredBackend: configuredBackend,
		FrontendID: a.FrontendID(), FrontendConfigIndex: a.FrontendConfigIndex(), State: a.State(),
		Feishu: a.Feishu(), EffectRunner: *a.runtimeOwner.EffectRunner,
		WorkspacePresentation: a.bindings.WorkspacePresentation, WorkspaceConfiguration: a.bindings.WorkspaceConfiguration,
		Workflow: a.bindings.UpgradeWorkflow,
	})
	actors, replayRunner := BindingReplayPorts(a.runtimeOwner.SessionActors, a.runtimeOwner)
	a.bindings.BindingReplay = runtime.BindingReplay{Service: a.bindings.BindingPending, Runner: replayRunner, Actors: actors}
	a.bindings.WorkspaceEffects = workspaceapp.EffectService{Lifecycle: a.bindings.WorkspaceCreation.Lifecycle, Runtime: WorkspaceEffectRuntime(&a.runtimeOwner.Lifecycle, a.asyncRunner, actors, a.runtimeOwner.LiveThreads, a.bindings.BindingReplay), Conversations: a.bindings.Conversations, Context: a.Context}
	a.bindings.WorkspaceWorkflow.Effects = a.bindings.WorkspaceEffects
	a.bindings.GroupWorkspaces = workspaceapp.GroupService{Frontend: identity.FrontendID(a.FrontendID()), Repository: a.State(), Creation: a.bindings.WorkspaceCreation, Planning: a.bindings.WorkspacePlanning, Effects: a.bindings.WorkspaceEffects}
	a.bindings.BindingCommands = BuildBindingCommands(bindingCommandInputsForTest(a, bindingScope))
	a.bindings.ConversationRecovery = conversation.NewRecovery(ConversationRecoveryPorts(
		a.Config(), a.ConfigMu(), a.FrontendConfigIndex(), a.State(),
		a.bindings.Conversations, a.runtimeOwner, a.bindings.CodexRecovery, a.bindings.ConversationConfiguration,
	))
	a.bindings.StartupRecovery = maintenance.NewStartupRecovery(StartupRecoveryPorts(StartupRecoveryPortInputs{
		Runtime: a.BackendRuntimeDeps(), StartupState: a.bindings.StartupState,
		CleanupExpiredAttachments: func() { a.bindings.MaintenanceCommands.CleanupExpiredAttachments() },
		RestoreConversationState:  a.bindings.ConversationRecovery.Restore,
		SendText:                  newEffectOutbound(a.FrontendID(), *a.runtimeOwner.EffectRunner).SendText,
	}))
	recoverFrontendRuntimeState = a.bindings.StartupRecovery.RecoverFrontendRuntimeState
	backendSwitch := backendselection.NewService(BackendSwitchPorts(BackendSwitchPortInputs{
		RuntimeDeps: a.BackendRuntimeDeps(), Transition: &a.runtimeOwner.BackendTransition,
		FrontendQuery: a.bindings.FrontendQuery, StartupRecovery: a.bindings.StartupRecovery,
		Announcements: a.runtimeOwner.Announcements, AnnouncementQuery: a.bindings.AnnouncementQuery,
	}))
	a.bindings.BackendSwitch = &backendSwitch
	a.bindings.BackendSelection = BuildBackendSelection(a)
	inboundBackend := ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex())
	inboundSessionKey := SessionKeyBuilder(a.FrontendID())
	inboundService.Deps = InboundPorts(InboundPortInputs{
		FrontendID: a.FrontendID(), Context: a.Context, SessionKey: inboundSessionKey,
		Feishu: a.Feishu(), Primary: a.bindings.Primary, PrimaryInitialization: a.bindings.PrimaryInitialization,
		GroupMessages: a.bindings.GroupMessages, Requests: a.bindings.ServerRequests,
		InteractionLifecycle:  a.bindings.InteractionLifecycle,
		CompleteWorkspaceText: a.bindings.WorkspaceManagement.CompleteWorkspaceNewText,
		ClaudeSupport:         a.bindings.ClaudeSupport, Continuation: a.bindings.Continuation,
		PendingQueue: a.bindings.PendingQueue, Config: a.Config(), BindingPending: a.bindings.BindingPending,
		StoreReady: a.store != nil, StateReady: a.State() != nil,
		GateContext: a.runtimeOwner.Lifecycle.Context, Effects: *a.runtimeOwner.EffectRunner,
		WorkspaceMenu: a.bindings.WorkspacePresentation.RenderWorkspaceMenuCard,
		LocalBackend:  inboundBackend,
		HandleCommand: func(msg *application.InboundMessage, text string) error {
			return HandleInboundCommand(a, msg, text)
		},
		SelectBackend: a.bindings.BackendSelection.ReplyBackendSelectionCard,
		BlockedReason: a.runtimeOwner.BackendTransition.BackendSwitchBlockedReasonForTraffic,
		RuntimeDeps:   a.BackendRuntimeDeps(), Queue: a.bindings.Submissions,
		RefreshGroup: func(chatID, _ string) { scheduleGroupAnnouncementStatusRefresh(a.runtimeOwner.Announcements, chatID) },
		FlushNotifications: func(msg *application.InboundMessage) {
			if msg != nil && a.Feishu() != nil && a.store != nil {
				a.bindings.Notifications.Flush(msg.ChatID, msg.UserID)
			}
		},
		PrefetchForward: forwardService.Start,
	})
	a.bindings.Inbound = inboundService
	controls := conversation.NewControls(ConversationControlPorts(ConversationControlInputs{
		Repository: a.State(), Config: a.Config(), ConfigMu: a.ConfigMu(),
		FrontendID: a.FrontendID(), FrontendConfigIndex: a.FrontendConfigIndex(),
		Conversations: a.bindings.Conversations, Pending: a.bindings.PendingQueue,
		RetryTracker: a.runtimeOwner.AutoRetries, AutoRetry: a.bindings.AutoRetry,
		Runtime: a.BackendRuntimeDeps(), Context: a.Context,
	}))
	a.bindings.ConversationControls = &controls
	a.bindings.ThreadMenu = BuildThreadMenu(a)
	planSource, planCatalog, planWorkspaces := PlanPorts(a.Config(), a.ConfigMu(), a.bindings.ModelSnapshots, a.runtimeOwner)
	*a.bindings.Plan = planapp.Service{Forms: a.bindings.Forms, Delivery: a.bindings.InteractionDelivery, Repository: a.State(), Settings: planapp.SettingsService{Source: planSource, Catalog: planCatalog, Context: a.Context}, Conversations: a.bindings.Conversations, Workspaces: planWorkspaces, Queue: a.bindings.Submissions}
	*turnPlanMode = PlanModePorts(PlanModePortInputs{
		Runtime: a.BackendRuntimeDeps(), UseCase: a.bindings.Plan, Continuation: a.bindings.Continuation,
		State: a.State(), ModelSnapshots: a.bindings.ModelSnapshots,
		WorkspaceSelection: a.bindings.WorkspaceSelection, Submissions: a.bindings.Submissions,
		Conversations: a.bindings.Conversations, Feishu: a.Feishu(), AsyncRunner: a.AsyncRunner(),
	})
	a.bindings.ReviewCommands = BuildReviewCommands(a)
	cardActionFrontendID := a.FrontendID()
	normalizeCardActionSessionKey := func(key string) string {
		return identity.CanonicalSessionKey(cardActionFrontendID, key)
	}
	a.bindings.CardActions = cardaction.NewService(CardActionPorts(
		a, normalizeCardActionSessionKey,
		a.runtimeOwner.BackendTransition.BackendSwitchBlocksCardAction,
		a.bindings.WorkspaceConfiguration.WorkspaceDeleteActions(),
		a.bindings.History,
		a.bindings.ServerRequests, a.bindings.ClaudeSupport, a.bindings.ReviewCommands,
		a.bindings.Upgrades, a.bindings.BackendUpgrades, PathPickerActionInputs{
			State: a.State(), Forms: a.bindings.Forms, Picker: a.bindings.PathPicker,
			Planning: a.bindings.WorkspacePlanning, WorkspaceCards: a.bindings.WorkspacePresentation,
			Upgrades: a.bindings.Upgrades, Debug: a.bindings.Debug,
			SimpleStatusCard: func(title, color, body string, buttons []feishu.Button) map[string]any {
				if client := a.Feishu(); client != nil {
					return client.SimpleStatusCard(title, color, body, buttons)
				}
				return nil
			},
		}, a.bindings.ThreadMenu, *turnPlanMode, AsyncUserInputActionInputs{
			State: a.State(), Inputs: a.bindings.AsyncInputs, Context: a.Context,
			FrontendID: a.FrontendID(), EffectRunner: newEffectRunner(a.runtimeOwner),
			SimpleStatusCard: func(title, color, body string, buttons []feishu.Button) map[string]any {
				if client := a.Feishu(); client != nil {
					return client.SimpleStatusCard(title, color, body, buttons)
				}
				return nil
			},
		}, PendingFormCancelActionInputs{
			State: a.State(), ServerRequests: a.bindings.ServerRequests,
			FinalizePending:   a.bindings.PendingReplies.Finalize,
			WorkspaceMenuCard: a.bindings.WorkspacePresentation.RenderWorkspaceMenuCard,
			SimpleStatusCard: func(title, color, body string, buttons []feishu.Button) map[string]any {
				if client := a.Feishu(); client != nil {
					return client.SimpleStatusCard(title, color, body, buttons)
				}
				return nil
			},
			WorkspaceConfigured: a.Config() != nil,
		},
	))
	dispatcher := NewDispatcher(testDispatcherInputs(a))
	a.runtimeOwner.Dispatcher = &dispatcher
	if a.runtimeOwner.MCP == nil {
		mcp, err := buildMCPForTest(a)
		if err != nil {
			panic(err)
		}
		a.bindings.MCP = mcp
		a.runtimeOwner.MCP = runtime.NewResource(mcp)
	}
	*autoRetryRuntimeDeps = a.BackendRuntimeDeps()
	return a
}

func renderThreadsCardForTest(a *App, key string, all bool) (map[string]any, error) {
	backend := ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex())
	return renderThreadsCard(threadCardInputs{
		Repository: a.State(), Config: a.Config(), Backend: backend, Conversations: a.bindings.Conversations,
	}, key, all)
}

func recomposeTestApp(a *App) {
	a.stateView = testStateView(a)
	a.transport = a.feishu
	a.runtimeOwner.EffectRunner = nil
	a.runtimeOwner.Dispatcher = nil
	prepareTestApp(a)
}

func testSelectedBackendOwner(owner *runtime.FrontendOwner, backend string) *runtime.FrontendOwner {
	if owner == nil {
		owner = runtime.NewFrontendOwner()
	}
	owner.SetBackend(backend)
	return owner
}

func bindingCommandInputsForTest(a *App, scope BindingScope) BindingCommandInputs {
	client := a.feishu
	return BindingCommandInputs{
		Scope: scope, Config: a.Config(), ConfigMu: a.ConfigMu(), State: a.State(), FrontendID: a.FrontendID(),
		ConfiguredBackend: ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.FrontendConfigIndex()),
		MakeSessionKey:    SessionKeyBuilder(a.FrontendID()), Context: a.Context, Effects: newEffectRunner(a.runtimeOwner),
		RunAsync: func(fn func()) { a.runtimeOwner.Lifecycle.Run(fn, a.asyncRunner) },
		RefreshGroupStatus: func(chatID string) {
			if a.runtimeOwner.Announcements != nil {
				a.runtimeOwner.Announcements.Schedule(chatID)
			}
		},
		CurrentBotDisplayName: BotDisplayName(client), CurrentLiveBotOpenID: LiveBotOpenID(client),
		InitializeGroupPrimary: func(ctx context.Context, chatType, chatID string) error {
			return EnsureGroupPrimary(ctx, a.bindings.PrimaryInitialization, a.FrontendID(), client, chatType, chatID)
		},
		SimpleStatusCard: func(title, color, body string, buttons []feishu.Button) map[string]any {
			if client == nil {
				return nil
			}
			return client.SimpleStatusCard(title, color, body, buttons)
		},
		BackendConfiguration: a.bindings.BackendConfiguration, Forms: a.bindings.Forms, FrontendQuery: a.bindings.FrontendQuery,
		GroupWorkspaces: a.bindings.GroupWorkspaces, ModelCommands: a.bindings.ModelCommands, ModelSnapshots: a.bindings.ModelSnapshots,
		Primary: a.bindings.Primary, RoutingConfiguration: a.bindings.RoutingConfiguration, ScopedRoutingConfiguration: a.bindings.ScopedRoutingConfiguration,
		ServiceTier: a.bindings.ServiceTier, WorkspaceConfiguration: a.bindings.WorkspaceConfiguration,
		WorkspaceManagement: a.bindings.WorkspaceManagement, WorkspacePresentation: a.bindings.WorkspacePresentation, WorkspaceWorkflow: a.bindings.WorkspaceWorkflow,
	}
}
