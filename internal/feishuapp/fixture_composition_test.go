package feishuapp

import (
	"context"
	configadapter "feidex/internal/adapter/config"
	"feidex/internal/adapter/feishu/approval"
	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/finalcardpatch"
	"feidex/internal/adapter/feishu/goalcmd"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/adapter/feishu/turnmeta"
	"feidex/internal/adapter/feishu/turnstream"
	"feidex/internal/adapter/feishu/upgraderender"
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

// Focused fixtures explicitly construct their complete dependency graph.
func prepareTestApp(a *App) *App {
	if a.runtimeOwner == nil {
		a.runtimeOwner = runtime.NewFrontendOwner()
	}
	if a.transport == nil {
		a.transport = a.feishu
	}
	if a.stateView == nil {
		a.stateView = NewStateView(a)
	}
	if a.runtimeOwner.EffectRunner == nil {
		runner := buildEffectRunner(a)
		a.runtimeOwner.EffectRunner = &runner
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
	a.bindings.RuntimeSettings = runtimeconfig.Service{Repository: configadapter.NewRuntimeRepository(a)}
	a.bindings.PathPicker = pathpicker.Service{Filesystem: filesystempicker.Filesystem{}}
	a.bindings.AsyncInputs = asyncinput.Service{Deps: asyncinput.Dependencies{Repository: a.State(), Backend: func() string { return a.configView().configuredBackend() }, Context: a.Context, Run: SessionTaskRunner(a), Effects: newEffectRunner(a.runtimeOwner)}}
	a.bindings.WorkspaceSelection = workspaceapp.SelectionService{Frontend: identity.FrontendID(a.FrontendID()), Repository: scoped.WorkspaceSelections{Store: a.State()}, DefaultWorkspaceID: DefaultWorkspaceID(a)}
	a.bindings.GoalManagement = &goal.Management{Tracker: a.bindings.Goals, Context: a.Context, Gateway: func() (goal.Gateway, error) { return RequireCodexGoalGateway(a) }}
	a.bindings.SubmissionLookup = submission.SubmissionLookupService{State: a.State(), Runtime: a.runtimeOwner.TurnBindings}
	a.bindings.SubmissionStatus = submission.StatusService{Lookup: a.bindings.SubmissionLookup, Repository: a.State()}
	a.bindings.InteractionLifecycle = interaction.LifecycleService{Repository: a.State(), Frontend: a.FrontendID(), Presentation: InteractionExpiryPresentation(a)}
	a.bindings.ModelAcknowledgements = modelconfig.AcknowledgementService{Repository: a.State()}
	a.bindings.ClaudeFactory = func(cfg config.ClaudeConfig) ClaudeCore { return clauderuntime.NewService(ClaudeRuntimePorts(a, cfg)) }
	routingConfiguration := routing.ConfigurationService{Repository: a.State(), Frontend: identity.FrontendID(a.FrontendID())}
	a.bindings.RoutingConfiguration = compositionkit.RoutingConfiguration{ConfigurationService: routingConfiguration, Runner: newEffectRunner(a.runtimeOwner), Context: a.Context()}
	a.bindings.ScopedRoutingConfiguration = compositionkit.ScopedRoutingConfiguration{Service: routing.ScopedConfigurationService{ConfigurationService: routingConfiguration, BackendSource: func() string { return a.configView().configuredBackend() }}, Runner: newEffectRunner(a.runtimeOwner), Context: a.Context()}
	primaryRepository := statejson.NewGroupPrimaryRepository(a.Store(), a.FrontendID())
	a.bindings.Primary = routing.Service{Repository: primaryRepository}
	a.bindings.GroupMessages = routing.GroupMessages{Frontend: a.FrontendID(), Primary: a.bindings.Primary, Links: a.State(), SelfOpenID: func() string { return currentLiveBotOpenID(a) }}
	a.bindings.Announcements = announcement.Service{Repository: a.State(), Gateway: AnnouncementGateway(a.feishu), Frontend: a.FrontendID(), Primary: func(chatID string) bool {
		enabled, _ := a.bindings.Primary.IsPrimary(a.FrontendID(), "group", chatID)
		return enabled
	}}
	a.bindings.AnnouncementQuery = announcement.Query{Repository: a.State(), Workspaces: configadapter.NewWorkspaceRepository(a), HasPrimary: func(chatID string) bool {
		record, _ := a.bindings.Primary.Lookup(a.FrontendID(), "group", chatID)
		return record != nil
	}}
	a.bindings.PrimaryInitialization = routing.InitializationService{Repository: primaryRepository, BotCount: func(ctx context.Context, chatID string) (int, error) { return a.feishu.GetGroupBotCount(ctx, chatID) }, LiveBotOpenID: func() string { return currentLiveBotOpenID(a) }}
	a.bindings.TurnMetadata = turnmeta.Service{Tracker: a.runtimeOwner.TurnBindings}
	a.bindings.ItemContext = approval.ItemContext{Items: a.bindings.TurnItems, Started: func(threadID, turnID string) { a.bindings.Turns.BindPendingSubmissionTurn(threadID, turnID, true) }}
	a.bindings.PendingReplies = PendingReplyAdapter{Service: a.bindings.Interactions, Repository: a.State()}
	filesystem, git := WorkspaceCreationPorts(a.ConfigPath())
	workspaceLifecycle := &workspaceapp.Lifecycle{Frontend: identity.FrontendID(a.FrontendID()), Selection: a.WorkspaceSelection(), Configuration: workspaceapp.ConfigurationService{Repository: configadapter.NewWorkspaceRepository(a)}, Repository: configadapter.WorkspaceLifecycleRepository{Source: a, Scope: a.State()}}
	a.bindings.WorkspaceCreation = &workspaceapp.CreationService{Filesystem: filesystem, Git: git, Lifecycle: workspaceLifecycle}
	a.bindings.WorkspaceSettings = workspaceapp.SettingsService{Frontend: a.FrontendID(), Repository: configadapter.WorkspaceSettingsRepository{Source: a, Scope: a.State()}}
	a.bindings.WorkspacePlanning = &workspaceapp.PlanningService{Repository: configadapter.NewWorkspaceRepository(a), PendingRequests: a.State().PendingRequests, ConfigPath: a.ConfigPath, BotName: func() string { return currentBotDisplayName(a) }, FrontendID: a.FrontendID, Paths: runtimeworkspace.PlanningFilesystem{}, Git: runtimeworkspace.PlanningGit{}}
	a.bindings.Forms = &interaction.FormService{Repository: a.State()}
	a.bindings.WorkspaceWorkflow = &workspaceapp.Workflow{Forms: a.bindings.Forms, Planning: a.bindings.WorkspacePlanning, Creation: a.bindings.WorkspaceCreation}
	a.bindings.WorkspacePresentation = NewWorkspacePresentation(a)
	a.bindings.ModelCommands = BuildModelCommands(a)
	a.bindings.BindingCommands = BuildBindingCommands(a)
	a.bindings.BackendUpgrades = BuildBackendUpgrades(a)
	a.bindings.UpgradePresentation = BuildUpgradePresentation(a)
	platform, releases, artifacts, launcher := UpgradeWorkflowPorts(a.Config(), a.ConfigMu(), a.runtimeOwner)
	a.bindings.UpgradeWorkflow = &upgrade.Service{Forms: a.bindings.Forms, Platform: platform, Releases: releases, Artifacts: artifacts, Launcher: launcher}
	a.bindings.Upgrades = BuildUpgrades(a)

	a.bindings.Maintenance = backendmaintenance.NewMaintenanceStateService(a.runtimeOwner.MaintenanceTrackers, MaintenanceRepository(a))
	a.bindings.StartupState = conversation.StartupState{Repository: a.State(), DefaultWorkspaceID: func() string { return a.WorkspaceSelection().ResolveSession(nil) }}
	a.bindings.UpgradePoller = upgrade.Poller{Repository: a.State(), Units: upgradeunits.Units{}}
	a.bindings.StartupRecovery = maintenance.NewStartupRecovery(StartupRecoveryPorts(a, func() {
		a.bindings.MaintenanceCommands.CleanupExpiredAttachments()
	}))
	a.bindings.MaintenanceCommands = BuildMaintenanceCommands(a)
	a.bindings.SubmissionCleanup = maintenance.SubmissionCleanup{Repository: a.State(), Runtime: a.runtimeOwner.TurnBindings, Items: a.bindings.TurnItems}
	a.bindings.AutoRetry = AutoRetryView(a)
	a.bindings.AutoRetry.Engine = retry.NewEngine(AutoRetryPorts(a, a.bindings.AutoRetry))
	a.bindings.FrontendQuery = frontendapp.Query{Repository: a.State(), Facts: FrontendFacts(a), Retrying: a.bindings.AutoRetry.HasBlockingAutoRetry}
	var codexUpgrade codexruntime.UpgradeService
	a.bindings.CodexRecovery = codexruntime.NewRecoveryService(CodexRecoveryPorts(a,
		func(ctx context.Context) (codexruntime.CodexClient, error) {
			return codexUpgrade.StartVerifiedCodexClient(ctx)
		},
		func() { a.bindings.StartupRecovery.RecoverFrontendRuntimeState() },
	))
	codexUpgrade = codexruntime.NewUpgradeService(CodexUpgradePorts(
		a.Config(), a.ConfigMu(), a.FrontendID(), a.FrontendConfigIndex(),
		a.runtimeOwner, a.BackendRuntimeDeps(), a.bindings.CodexRecovery,
		func() { _ = a.bindings.StartupRecovery.RecoverFrontendRuntimeState() },
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
	a.bindings.History = BuildHistory(a)
	sharedArtifacts, downloadPresentation, downloadRunner := FileSharePorts(a)
	a.bindings.FileSharing = &fileshare.Service{Forms: a.bindings.Forms, Repository: a.State(), Artifacts: sharedArtifacts, Presentation: downloadPresentation, Context: a.Context, Run: downloadRunner}
	a.bindings.Debug = BuildDebug(a)
	a.bindings.Usage = BuildUsage(a)
	a.bindings.FinalCardPatch = BuildFinalCardPatch(a)
	a.bindings.ClaudeSupport = BuildClaudeSupport(a)
	a.bindings.ThreadSettings = threadsettings.Service{Repository: a.State()}
	a.bindings.PermissionSettings = threadsettings.PermissionService{Settings: a.bindings.ThreadSettings, Source: configadapter.ThreadPermissionRepository{Source: a, Scope: a.State()}, Runtime: PermissionRuntime(a), Tasks: PermissionTasks(a), Failure: PermissionFailure(a), Context: a.Context}
	a.bindings.ServiceTier = BuildServiceTier(a)
	a.bindings.ModelSnapshots = modelconfig.SnapshotService{Repository: ModelSnapshotRepository(a)}
	a.bindings.ModelSettings = modelconfig.SettingsService{Repository: a.State(), Admission: ModelWriteAdmission(a), Frontend: identity.FrontendID(a.FrontendID())}
	a.bindings.ModelDefaults = modelconfig.DefaultsService{Repository: configadapter.ModelDefaultsRepository{Source: a, Scope: a.State()}, Admission: ModelWriteAdmission(a), Frontend: a.FrontendID(), Publisher: ModelDefaultsPublisher(a)}
	a.bindings.ModelOptions = modelconfig.OptionsService{Repository: configadapter.ModelOptionsRepository{Source: a}}
	a.bindings.ConversationConfiguration = conversation.Configuration{Models: a.bindings.ModelSnapshots, ServiceName: CodexServiceName(a)}
	a.bindings.TurnStarter = submission.TurnStarter{Frontend: identity.FrontendID(a.FrontendID()), Effects: newEffectRunner(a.runtimeOwner), Collaboration: a.bindings.Plan}
	a.bindings.BindingPending = routing.PendingService{Configuration: a.bindings.RoutingConfiguration.ConfigurationService, Repository: a.State()}
	a.bindings.BackendConfiguration = BuildBackendConfiguration(a)
	a.bindings.BackendActions = BuildBackendActions(a)
	backendSwitch := backendselection.NewService(BackendSwitchPorts(a))
	a.bindings.BackendSwitch = &backendSwitch
	a.bindings.BackendSelection = BuildBackendSelection(a)
	a.bindings.ServerRequests = BuildServerRequests(a)
	a.bindings.Skills = compositionkit.NewSkillService(SkillUseCasePorts(a.Config(), a.ConfigMu(), a.Context, a.State(), a.runtimeOwner.PendingSkills, a.FrontendID(), a.runtimeOwner))
	a.bindings.SkillCommands = BuildSkillCommands(a)
	*a.bindings.PendingQueue = submission.NewPendingQueueService(PendingQueuePorts(a.Context, a.State(), a.bindings.SubmissionCleanup, a.Config(), a.ConfigMu(), a.Feishu()))
	a.bindings.Continuation.Deps = ContinuationPorts(a.Config(), a.ConfigMu(), a.Context, a.State(), a.runtimeOwner, a.bindings.Submissions, a.FrontendID(), a.FrontendConfigIndex(), a.Feishu())
	a.bindings.Compaction.Deps = CompactionPorts(a.Context, a.State(), a.runtimeOwner, a.FrontendID(), a.feishu != nil)
	a.bindings.GoalContinuation.Deps = GoalContinuationPorts(a)
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
	review := reviewapp.NewService(ReviewPorts(a))
	a.bindings.Review = &review
	*a.bindings.Submissions = submission.NewSubmissionQueueService(SubmissionPorts(a, a.bindings.Plan, a.bindings.TurnPresentation))
	*a.bindings.Turns = turn.NewService(TurnPorts(a, a.bindings.TurnPresentation))
	*a.bindings.TurnPresentation = turnstream.NewService(TurnPresentationPorts(a, a.bindings.Turns))
	a.bindings.TurnReconciliation = turn.Reconciliation{Gateway: TurnReconciliationGateway(a), Session: a.State().Session, SawFinal: a.bindings.TurnPresentation.StreamSawFinal, Finish: a.bindings.Turns.FinishTurn, Context: a.Context}
	a.bindings.ClaudeReconciliation = turn.StoppedReconciliation{Stopped: ClaudeSessionStopped(a), Session: a.State().Session, Finish: a.bindings.Turns.FinishTurn}
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
	failure := backendfailure.NewBackendFailureService(BackendFailurePorts(a))
	a.bindings.BackendFailure = &failure
	inboundService := &inbound.Service{}
	forwardService := inbound.ForwardService{Gateway: ForwardGateway(a), Tasks: ForwardTasks(a), Context: a.Context, Process: ForwardProcessor(a, func(msg *application.InboundMessage) error { return inboundService.ProcessMessage(msg) }), Queued: a.bindings.PendingQueue.MarkMessagesQueuedReactions, Clear: a.bindings.PendingQueue.ClearMessageProcessingReactions, Failed: ForwardFailure(a)}
	inboundService.Deps = InboundPorts(a, forwardService.Start)
	a.bindings.Inbound = inboundService
	a.bindings.ForwardInputs = forwardService
	a.bindings.Conversations = &conversation.Service{Deps: ConversationPorts(a)}
	a.bindings.WorkspaceConfiguration = BuildWorkspaceConfiguration(a, a.bindings.WorkspacePresentation, a.bindings.Conversations)
	a.bindings.WorkspaceManagement = BuildWorkspaceManagement(a, a.bindings.WorkspacePresentation, a.bindings.Conversations)
	a.bindings.WorkspaceEffects = workspaceapp.EffectService{Lifecycle: a.bindings.WorkspaceCreation.Lifecycle, Runtime: WorkspaceEffectRuntime(a), Conversations: a.bindings.Conversations, Context: a.Context}
	a.bindings.WorkspaceWorkflow.Effects = a.bindings.WorkspaceEffects
	a.bindings.GroupWorkspaces = workspaceapp.GroupService{Frontend: identity.FrontendID(a.FrontendID()), Repository: a.State(), Creation: a.bindings.WorkspaceCreation, Planning: a.bindings.WorkspacePlanning, Effects: a.bindings.WorkspaceEffects}
	actors, replayRunner := BindingReplayPorts(a.sessionActorRuntime(), a.runtimeOwner)
	a.bindings.BindingReplay = runtime.BindingReplay{Service: a.bindings.BindingPending, Runner: replayRunner, Actors: actors}
	a.bindings.ConversationQuery = conversation.Query{Repository: a.State()}
	a.bindings.Notifications = frontendapp.Notifications{Repository: a.State(), Sender: NotificationSender(a), Context: a.Context}
	a.bindings.ConversationRecovery = conversation.NewRecovery(ConversationRecoveryPorts(
		a.Config(), a.ConfigMu(), a.FrontendConfigIndex(), a.State(),
		a.bindings.Conversations, a.runtimeOwner, a.bindings.CodexRecovery, a.bindings.ConversationConfiguration,
	))
	controls := conversation.NewControls(ConversationControlPorts(a))
	a.bindings.ConversationControls = &controls
	a.bindings.ThreadMenu = BuildThreadMenu(a)
	planSource, planCatalog, planWorkspaces := PlanPorts(a.Config(), a.ConfigMu(), a.bindings.ModelSnapshots, a.runtimeOwner)
	*a.bindings.Plan = planapp.Service{Forms: a.bindings.Forms, Delivery: a.bindings.InteractionDelivery, Repository: a.State(), Settings: planapp.SettingsService{Source: planSource, Catalog: planCatalog, Context: a.Context}, Conversations: a.bindings.Conversations, Workspaces: planWorkspaces, Queue: a.bindings.Submissions}
	a.bindings.ReviewCommands = BuildReviewCommands(a)
	cardActionFrontendID := a.FrontendID()
	normalizeCardActionSessionKey := func(key string) string {
		return identity.CanonicalSessionKey(cardActionFrontendID, key)
	}
	a.bindings.CardActions = cardaction.NewService(CardActionPorts(
		a, normalizeCardActionSessionKey,
		a.runtimeOwner.BackendTransition.BackendSwitchBlocksCardAction,
		a.bindings.WorkspaceConfiguration.WorkspaceDeleteActions(),
		a.bindings.ServerRequests, a.bindings.ClaudeSupport, a.bindings.ReviewCommands,
		a.bindings.Upgrades, a.bindings.BackendUpgrades,
	))
	dispatcher := newInputDispatcher(a)
	a.runtimeOwner.Dispatcher = &dispatcher
	if a.runtimeOwner.Announcements == nil {
		a.runtimeOwner.Announcements = runtime.NewCoalescedRefresh(&a.runtimeOwner.Lifecycle, 2*time.Second, 15*time.Second, GroupAnnouncementRefresh(a))
	}
	if a.runtimeOwner.MCP == nil {
		mcp, err := BuildMCP(a)
		if err != nil {
			panic(err)
		}
		a.bindings.MCP = mcp
		a.runtimeOwner.MCP = runtime.NewResource(mcp)
	}
	return a
}

func recomposeTestApp(a *App) {
	a.stateView = NewStateView(a)
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
