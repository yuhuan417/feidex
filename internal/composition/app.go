// Package composition is the production composition root.
package composition

import (
	"context"
	"feidex/internal/application"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	codexadapter "feidex/internal/adapter/backend/codex"
	configadapter "feidex/internal/adapter/config"
	"feidex/internal/adapter/feishu/approval"
	appbackend "feidex/internal/adapter/feishu/backend"
	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	"feidex/internal/adapter/feishu/finalcardpatch"
	"feidex/internal/adapter/feishu/goalcmd"
	appmenuutil "feidex/internal/adapter/feishu/menuutil"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/adapter/feishu/planmode"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	"feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/adapter/feishu/turnmeta"
	"feidex/internal/adapter/feishu/turnstream"
	"feidex/internal/adapter/feishu/upgraderender"
	filesystempicker "feidex/internal/adapter/filesystem/pathpicker"
	statejson "feidex/internal/adapter/storage/json"
	scoped "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/app"
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
	feishuapp "feidex/internal/feishuapp"
	"feidex/internal/runtime"
	clauderuntime "feidex/internal/runtime/claude"
	codexruntime "feidex/internal/runtime/codex"
	"feidex/internal/runtime/maintenance"
	"feidex/internal/runtime/turnbinding"
	upgradeunits "feidex/internal/runtime/upgrade"
	runtimeworkspace "feidex/internal/runtime/workspace"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type FrontendScope = runtime.FrontendScope

type Factory[T runtime.ManagedFrontend] func(FrontendScope) (T, error)
type Service[T runtime.ManagedFrontend] struct {
	runtime.FrontendGroup
	Frontends []T
}

func NewFrontend(scope FrontendScope) (*feishuapp.App, error) {
	if scope.StartedAt.IsZero() {
		scope.StartedAt = time.Now()
	}
	frontend, err := feishuapp.NewFeishuShell(scope)
	if err != nil {
		return nil, err
	}
	store := scope.Store
	configPath := scope.ConfigPath
	frontendConfigIndex := scope.Frontend.ConfigIndex
	configSource := configadapter.FrontendSource{
		WorkspaceSource: configadapter.WorkspaceSource{Value: scope.Config, Mu: scope.ConfigMutex, Path: configPath},
		ConfigIndex:     frontendConfigIndex,
	}
	var asyncRunner func(func())
	configuredBackend := feishuapp.ConfiguredBackendBuilder(
		scope.Config, scope.ConfigMutex, scope.RuntimeOwner.Backend, scope.Frontend.ID, scope.Frontend.ConfigIndex,
	)
	// All production objects are assembled here. internal/app only binds the
	// already composed Feishu event transport to the frontend entrypoint.
	scope.RuntimeOwner.TurnBindings = turnbinding.NewTracker(frontend.State().Submission)
	bindings := &feishuapp.Bindings{
		TurnStreams: turnstream.NewTracker(), TurnItems: turnitem.NewTracker(), FinalCardPatches: finalcardpatch.NewTracker(), Goals: goal.NewTracker(),
		Submissions: &submission.SubmissionQueueService{}, PendingQueue: &submission.PendingQueueService{},
		Continuation: &continuation.Service{}, Turns: &turn.Service{}, TurnPresentation: &turnstream.Service{},
		Compaction: &compaction.Service{}, GoalContinuation: &goal.Service{}, GoalCommands: &goalcmd.Service{},
		Interactions: &interaction.Service{}, BackendEvents: &backendevents.Service{}, Plan: &planapp.Service{},
	}
	bindings.GoalManagement = &goal.Management{Tracker: bindings.Goals, Context: frontend.Context, Gateway: func() (goal.Gateway, error) {
		return feishuapp.RequireCodexGoalGateway(scope.RuntimeOwner.CodexClient())
	}}
	frontend.AttachBindings(bindings)
	effectRunner := feishuapp.NewEffectRunner(feishuapp.EffectRunnerInputs{
		Transport: frontend.Feishu(), FrontendID: frontend.FrontendID(), State: frontend.State(),
		RuntimeOwner: scope.RuntimeOwner, Submissions: bindings.Submissions, AnnouncementRefresh: scope.RuntimeOwner.Announcements,
	})
	feishuapp.AttachEffectRunner(frontend, effectRunner)
	bindings.RuntimeSettings = runtimeconfig.Service{Repository: configadapter.NewRuntimeRepository(configSource)}
	bindings.PathPicker = pathpicker.Service{Filesystem: filesystempicker.Filesystem{}}
	bindings.AsyncInputs = asyncinput.Service{Deps: asyncinput.Dependencies{Repository: frontend.State(), Backend: configuredBackend, Context: frontend.Context, Run: feishuapp.SessionTaskRunner(scope.RuntimeOwner.SessionActors, func(fn func()) bool {
		return scope.RuntimeOwner.Lifecycle.Run(fn, asyncRunner)
	}), Effects: effectRunner}}
	bindings.WorkspaceSelection = workspaceapp.SelectionService{Frontend: identity.FrontendID(frontend.FrontendID()), Repository: scoped.WorkspaceSelections{Store: frontend.State()}, DefaultWorkspaceID: feishuapp.DefaultWorkspaceID(frontend.Config(), frontend.ConfigMu())}
	bindings.SubmissionLookup = submission.SubmissionLookupService{State: frontend.State(), Runtime: scope.RuntimeOwner.TurnBindings}
	bindings.SubmissionStatus = submission.StatusService{Lookup: bindings.SubmissionLookup, Repository: frontend.State()}
	bindings.InteractionLifecycle = interaction.LifecycleService{Repository: frontend.State(), Frontend: frontend.FrontendID(), Presentation: feishuapp.InteractionExpiryPresentation(frontend.Feishu(), frontend.State(), identity.FrontendID(frontend.FrontendID()), *scope.RuntimeOwner.EffectRunner)}
	bindings.ModelAcknowledgements = modelconfig.AcknowledgementService{Repository: frontend.State()}
	claudeRuntimeInputs := &feishuapp.ClaudeRuntimePortInputs{}
	bindings.ClaudeFactory = func(cfg config.ClaudeConfig) feishuapp.ClaudeCore {
		inputs := *claudeRuntimeInputs
		inputs.Config = cfg
		return clauderuntime.NewService(feishuapp.ClaudeRuntimePorts(inputs))
	}
	routingConfiguration := routing.ConfigurationService{Repository: frontend.State(), Frontend: identity.FrontendID(frontend.FrontendID())}
	bindings.RoutingConfiguration = compositionkit.RoutingConfiguration{ConfigurationService: routingConfiguration, Runner: effectRunner, Context: frontend.Context()}
	bindings.ScopedRoutingConfiguration = compositionkit.ScopedRoutingConfiguration{Service: routing.ScopedConfigurationService{ConfigurationService: routingConfiguration, BackendSource: configuredBackend}, Runner: effectRunner, Context: frontend.Context()}
	primaryRepository := statejson.NewGroupPrimaryRepository(store, frontend.FrontendID())
	bindings.Primary = routing.Service{Repository: primaryRepository}
	bindings.GroupMessages = routing.GroupMessages{Frontend: frontend.FrontendID(), Primary: bindings.Primary, Links: frontend.State(), SelfOpenID: feishuapp.LiveBotOpenID(frontend.Feishu())}
	bindings.Announcements = announcement.Service{Repository: frontend.State(), Gateway: feishuapp.AnnouncementGateway(frontend.Feishu()), Frontend: frontend.FrontendID(), Primary: func(chatID string) bool {
		enabled, _ := bindings.Primary.IsPrimary(frontend.FrontendID(), "group", chatID)
		return enabled
	}}
	bindings.AnnouncementQuery = announcement.Query{Repository: frontend.State(), Workspaces: configadapter.NewWorkspaceRepositoryForConfig(frontend.Config(), frontend.ConfigMu(), configPath), HasPrimary: func(chatID string) bool {
		record, _ := bindings.Primary.Lookup(frontend.FrontendID(), "group", chatID)
		return record != nil
	}}
	bindings.ConversationQuery = conversation.Query{Repository: frontend.State()}
	scope.RuntimeOwner.Announcements = runtime.NewCoalescedRefresh(&scope.RuntimeOwner.Lifecycle, 2*time.Second, 15*time.Second, feishuapp.GroupAnnouncementRefresh(feishuapp.GroupAnnouncementRefreshDependencies{
		FrontendID: frontend.FrontendID(), Feishu: frontend.Feishu(), Config: frontend.Config(), ConfigMu: frontend.ConfigMu(),
		FrontendConfigIndex: frontendConfigIndex, RuntimeOwner: scope.RuntimeOwner,
		Announcements: bindings.Announcements, AnnouncementQuery: bindings.AnnouncementQuery, ConversationQuery: bindings.ConversationQuery,
	}))
	liveThreads := feishuapp.SubmissionLiveThreads(scope.RuntimeOwner.LiveThreads, frontend.State().Session, bindings.AnnouncementQuery, scope.RuntimeOwner.Announcements.Schedule)
	bindings.PrimaryInitialization = routing.InitializationService{Repository: primaryRepository, BotCount: frontend.Feishu().GetGroupBotCount, LiveBotOpenID: feishuapp.LiveBotOpenID(frontend.Feishu())}
	bindings.TurnMetadata = turnmeta.Service{Tracker: scope.RuntimeOwner.TurnBindings}
	bindings.ItemContext = approval.ItemContext{Items: bindings.TurnItems, Started: func(threadID, turnID string) { bindings.Turns.BindPendingSubmissionTurn(threadID, turnID, true) }}
	bindings.PendingReplies = feishuapp.PendingReplyAdapter{Service: bindings.Interactions, Repository: frontend.State()}
	filesystem, git := feishuapp.WorkspaceCreationPorts(configPath)
	workspaceLifecycle := &workspaceapp.Lifecycle{Frontend: identity.FrontendID(frontend.FrontendID()), Selection: bindings.WorkspaceSelection, Configuration: workspaceapp.ConfigurationService{Repository: configadapter.NewWorkspaceRepository(configSource)}, Repository: configadapter.WorkspaceLifecycleRepository{Source: configSource, Scope: frontend.State()}}
	bindings.WorkspaceCreation = &workspaceapp.CreationService{Filesystem: filesystem, Git: git, Lifecycle: workspaceLifecycle}
	bindings.WorkspaceSettings = workspaceapp.SettingsService{Frontend: frontend.FrontendID(), Repository: configadapter.WorkspaceSettingsRepository{Source: configSource, Scope: frontend.State()}}
	bindings.WorkspacePlanning = &workspaceapp.PlanningService{Repository: configadapter.NewWorkspaceRepository(configSource), PendingRequests: frontend.State().PendingRequests, ConfigPath: func() string { return configPath }, BotName: frontend.Feishu().BotName, FrontendID: frontend.FrontendID, Paths: runtimeworkspace.PlanningFilesystem{}, Git: runtimeworkspace.PlanningGit{}}
	bindings.Forms = &interaction.FormService{Repository: frontend.State()}
	bindings.WorkspaceWorkflow = &workspaceapp.Workflow{Forms: bindings.Forms, Planning: bindings.WorkspacePlanning, Creation: bindings.WorkspaceCreation}
	workspacePresentation := NewWorkspacePresentation(WorkspacePresentationDependencies{
		Frontend: identity.FrontendID(scope.Frontend.ID), Config: scope.Config, ConfigPath: scope.ConfigPath,
		Mutex: scope.ConfigMutex, Scopes: frontend.State(), Backend: configuredBackend,
	})
	bindings.WorkspacePresentation = workspacePresentation
	bindings.ModelSnapshots = modelconfig.SnapshotService{Repository: feishuapp.ModelSnapshotRepository(frontend.Config(), frontend.ConfigMu(), frontend.State())}
	bindings.ModelDefaults = modelconfig.DefaultsService{
		Repository: configadapter.ModelDefaultsRepository{Source: configSource, Scope: frontend.State()},
		Admission:  feishuapp.ModelWriteAdmission(bindings.FrontendQuery), Frontend: frontend.FrontendID(),
		Publisher: feishuapp.ModelDefaultsPublisher(scope.RuntimeOwner, frontend.Config(), frontend.ConfigMu()),
	}
	bindings.ModelOptions = modelconfig.OptionsService{Repository: configadapter.ModelOptionsRepository{Source: configSource}}
	bindings.ModelCommands = feishuapp.BuildModelCommands(feishuapp.ModelCommandInputs{
		Defaults: &bindings.ModelDefaults, Options: &bindings.ModelOptions, Snapshots: bindings.ModelSnapshots,
		Config: frontend.Config(), ConfigMu: frontend.ConfigMu(), State: frontend.State(), RuntimeOwner: scope.RuntimeOwner,
		FrontendID: frontend.FrontendID(), FrontendConfigIndex: frontendConfigIndex,
		ConfiguredBackend: configuredBackend,
	})
	frontendIDForBindingScope := frontend.FrontendID()
	bindingScope := feishuapp.NewBindingScope(frontend.State(), func(key string) string {
		return identity.CanonicalSessionKey(frontendIDForBindingScope, key)
	}, bindings.Primary, frontendIDForBindingScope)
	platform, releases, artifacts, launcher := feishuapp.UpgradeWorkflowPorts(frontend.Config(), frontend.ConfigMu(), scope.RuntimeOwner)
	bindings.UpgradeWorkflow = &upgrade.Service{Forms: bindings.Forms, Platform: platform, Releases: releases, Artifacts: artifacts, Launcher: launcher}

	bindings.Maintenance = backendmaintenance.NewMaintenanceStateService(
		scope.RuntimeOwner.MaintenanceTrackers,
		feishuapp.MaintenanceRepository(frontend.State(), frontend.FrontendID()),
	)
	bindings.StartupState = conversation.StartupState{Repository: frontend.State(), DefaultWorkspaceID: func() string { return bindings.WorkspaceSelection.ResolveSession(nil) }}
	bindings.UpgradePoller = upgrade.Poller{Repository: frontend.State(), Units: upgradeunits.Units{}}
	bindings.Notifications = frontendapp.Notifications{Repository: frontend.State(), Sender: feishuapp.NotificationSender(frontend.Feishu(), frontend.FrontendID(), *scope.RuntimeOwner.EffectRunner), Context: frontend.Context}
	bindings.MaintenanceCommands = feishuapp.BuildMaintenanceCommands(feishuapp.MaintenanceCommandInputs{
		Context: scope.RuntimeOwner.Lifecycle.Context, Repository: frontend.State(), Poller: bindings.UpgradePoller,
		Feishu: frontend.Feishu(), FrontendID: frontend.FrontendID(), EffectRunner: *scope.RuntimeOwner.EffectRunner,
		Config: scope.Config, QueueNotification: bindings.Notifications.Queue, ReadyChatIDs: maintenance.StartupReadyChatIDs,
		RunAsync: func(fn func()) { scope.RuntimeOwner.Lifecycle.Run(fn, asyncRunner) },
	})
	bindings.SubmissionCleanup = maintenance.SubmissionCleanup{Repository: frontend.State(), Runtime: scope.RuntimeOwner.TurnBindings, Items: bindings.TurnItems}
	bindings.AutoRetry = feishuapp.AutoRetryView(feishuapp.AutoRetryViewInputs{
		Context: scope.RuntimeOwner.Lifecycle.Context, Config: scope.Config, ConfigMu: scope.ConfigMutex,
		ConfiguredBackend: configuredBackend, FrontendID: scope.Frontend.ID, FrontendConfigIndex: frontendConfigIndex,
		BackendDriver: appbackend.SelectedDriver{Selected: configuredBackend}, EffectRunner: *scope.RuntimeOwner.EffectRunner, Feishu: frontend.Feishu(),
	})
	// The queue captures AutoRetry by value before the dispatcher is wired.
	autoRetryRuntimeDeps := &feishuapp.BackendRuntimeDeps{}
	bindings.AutoRetry.Engine = retry.NewEngine(feishuapp.AutoRetryPorts(feishuapp.AutoRetryPortInputs{
		Context: frontend.Context, Tracker: scope.RuntimeOwner.AutoRetries, Repository: frontend.State(), Live: liveThreads,
		Enabled: func() bool { return bindings.AutoRetry.Settings().Enabled }, SaveEnabled: bindings.RuntimeSettings.SetAutoRetry,
		RuntimeDeps: autoRetryRuntimeDeps, RuntimeOwner: scope.RuntimeOwner,
		RunAsync:   func(fn func()) { scope.RuntimeOwner.Lifecycle.Run(fn, asyncRunner) },
		FrontendID: frontend.FrontendID(), Config: frontend.Config(), ConfigMu: frontend.ConfigMu(),
		Starter: bindings.Submissions, Presenter: bindings.AutoRetry,
	}))
	bindings.FrontendQuery = frontendapp.Query{Repository: frontend.State(), Facts: feishuapp.FrontendFacts(scope.RuntimeOwner, bindings.Maintenance), Retrying: bindings.AutoRetry.HasBlockingAutoRetry}
	var codexUpgrade codexruntime.UpgradeService
	var backendFailureOwner *backendfailure.BackendFailureService
	var recoverFrontendRuntimeState func() error
	recoverFrontend := func() {
		if recoverFrontendRuntimeState != nil {
			_ = recoverFrontendRuntimeState()
		}
	}
	bindings.CodexRecovery = codexruntime.NewRecoveryService(feishuapp.CodexRecoveryPorts(feishuapp.CodexRecoveryPortInputs{
		Runtime: frontend.BackendRuntimeDeps(), Submissions: bindings.Submissions, AsyncRunner: asyncRunner,
		BackendFailure: func() *backendfailure.BackendFailureService { return backendFailureOwner },
		StartVerifiedCodexClient: func(ctx context.Context) (codexruntime.CodexClient, error) {
			return codexUpgrade.StartVerifiedCodexClient(ctx)
		},
		RecoverFrontendRuntime: recoverFrontend,
	}))
	codexUpgrade = codexruntime.NewUpgradeService(feishuapp.CodexUpgradePorts(
		frontend.Config(), frontend.ConfigMu(), frontend.FrontendID(), frontendConfigIndex,
		scope.RuntimeOwner, frontend.BackendRuntimeDeps(), bindings.CodexRecovery,
		recoverFrontend,
		func(ctx context.Context) error { return codexUpgrade.CodexSmokeTest(ctx) },
	))
	bindings.CodexUpgrade = codexUpgrade
	smoke, active, current, create := feishuapp.ClaudeMaintenancePorts(frontend.Config(), frontend.ConfigMu(), frontend.Context, scope.RuntimeOwner, frontendConfigIndex, bindings.ClaudeFactory)
	bindings.ClaudeMaintenance = &clauderuntime.Maintenance{Smoke: smoke, Active: active, Current: current, Create: create}
	bindings.BackendMaintenance = make(map[string]*backendmaintenance.Service)
	bindings.MaintenanceRunners = make(map[string]maintenance.OperationRunner)
	maintenanceRenderer := frontend.Feishu()
	maintenanceFrontend := identity.FrontendID(frontend.FrontendID())
	maintenanceEffects := *scope.RuntimeOwner.EffectRunner
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
		installer, busy, backendRuntime, publisher := feishuapp.BackendMaintenancePorts(feishuapp.BackendMaintenancePortValues{
			Config: frontend.Config(), ConfigMu: frontend.ConfigMu(), Kind: kind,
			Frontend: identity.FrontendID(frontend.FrontendID()), State: bindings.Maintenance,
			CodexUpgrade: bindings.CodexUpgrade, ClaudeMaintenance: bindings.ClaudeMaintenance,
			RenderUpgrade: renderUpgrade, RenderRestart: renderRestart, PatchCard: patchCard,
		})
		service := &backendmaintenance.Service{Name: name, Kind: kind, Forms: bindings.Forms, Installer: installer, State: scope.RuntimeOwner.MaintenanceTrackers.Get(runtime.BackendKey(kind)), BusyReason: busy, Runtime: backendRuntime, Publisher: publisher}
		bindings.BackendMaintenance[kind] = service
		bindings.MaintenanceRunners[kind] = maintenance.OperationRunner{Lifecycle: &scope.RuntimeOwner.Lifecycle, Service: service, Executor: feishuapp.AsyncExecutor(asyncRunner)}
	}
	bindings.UpgradePresentation = feishuapp.BuildUpgradePresentation(maintenanceRenderer, bindings.BackendMaintenance)
	bindings.BackendUpgrades = feishuapp.BuildBackendUpgrades(feishuapp.BackendUpgradeInputs{
		SessionKey: feishuapp.SessionKeyBuilder(frontend.FrontendID()), ReplyInThread: func() bool { return false },
		FrontendID: identity.FrontendID(frontend.FrontendID()), Runner: *scope.RuntimeOwner.EffectRunner,
		Lifecycle: &scope.RuntimeOwner.Lifecycle, AsyncRunner: asyncRunner,
		BackendMaintenance: bindings.BackendMaintenance, MaintenanceRunners: bindings.MaintenanceRunners,
		Maintenance: bindings.Maintenance, Presentation: bindings.UpgradePresentation,
		Forms: bindings.Forms, CodexUpgrade: bindings.CodexUpgrade,
	})
	frontendID := identity.FrontendID(frontend.FrontendID())
	bindings.History = feishuapp.BuildHistory(
		frontendID, frontend.State(), feishuapp.ConfiguredBackendBuilder(frontend.Config(), frontend.ConfigMu(), scope.RuntimeOwner.Backend, frontend.FrontendID(), frontendConfigIndex),
		func() codexadapter.RPCClient { return scope.RuntimeOwner.CodexClient() },
		scope.RuntimeOwner.Lifecycle.Context, *scope.RuntimeOwner.EffectRunner,
		feishuapp.SessionKeyBuilder(frontend.FrontendID()), func(string) bool { return false },
	)
	debugViewDependencies := feishuapp.DebugViewDependencies(frontend)
	sharedArtifacts, downloadPresentation, downloadRunner := feishuapp.FileSharePorts(frontend.Feishu(), debugViewDependencies, &scope.RuntimeOwner.Lifecycle, scope.RuntimeOwner.SessionActors, asyncRunner)
	bindings.FileSharing = &fileshare.Service{Forms: bindings.Forms, Repository: frontend.State(), Artifacts: sharedArtifacts, Presentation: downloadPresentation, Context: frontend.Context, Run: downloadRunner}
	debugViewDependencies.FileSharing = bindings.FileSharing
	bindings.Debug = feishuapp.BuildDebug(debugViewDependencies)
	bindings.Usage = feishuapp.BuildUsage(debugViewDependencies)
	bindings.FinalCardPatch = feishuapp.BuildFinalCardPatch(feishuapp.FinalCardPatchInputs{
		Context: frontend.Context, Tracker: bindings.FinalCardPatches, Finder: frontend.State(),
		Patcher: frontend.Feishu(), RunAsync: asyncRunner, Config: frontend.Config(), State: frontend.State(),
	})
	bindings.ThreadSettings = threadsettings.Service{Repository: frontend.State()}
	permissionBackend := feishuapp.ConfiguredBackendBuilder(frontend.Config(), frontend.ConfigMu(), scope.RuntimeOwner.Backend, frontend.FrontendID(), frontendConfigIndex)
	permissionMenuRenderer := feishuapp.ClaudePermissionMenuRenderer(frontend.Config(), permissionBackend, frontend.State().Session)
	bindings.PermissionSettings = threadsettings.PermissionService{
		Settings: bindings.ThreadSettings,
		Source:   configadapter.ThreadPermissionRepository{Source: configSource, Scope: frontend.State()},
		Runtime:  feishuapp.PermissionRuntime(permissionBackend, scope.RuntimeOwner.ClaudeCore),
		Tasks:    feishuapp.PermissionTasks(&scope.RuntimeOwner.Lifecycle, scope.RuntimeOwner.SessionActors, asyncRunner),
		Failure:  feishuapp.PermissionFailure(permissionMenuRenderer, frontend.FrontendID(), *scope.RuntimeOwner.EffectRunner),
		Context:  frontend.Context,
	}
	bindings.ServiceTier = feishuapp.BuildServiceTier(
		bindings.ThreadSettings, frontend.Context, identity.FrontendID(frontend.FrontendID()),
		*scope.RuntimeOwner.EffectRunner, feishuapp.SessionKeyBuilder(frontend.FrontendID()),
	)
	bindings.ModelSettings = modelconfig.SettingsService{Repository: frontend.State(), Admission: feishuapp.ModelWriteAdmission(bindings.FrontendQuery), Frontend: identity.FrontendID(frontend.FrontendID())}
	bindings.ConversationConfiguration = conversation.Configuration{Models: bindings.ModelSnapshots, ServiceName: feishuapp.CodexServiceName(frontend.Config(), frontend.ConfigMu())}
	bindings.TurnStarter = submission.TurnStarter{Frontend: identity.FrontendID(frontend.FrontendID()), Effects: effectRunner, Collaboration: bindings.Plan}
	bindings.BindingPending = routing.PendingService{Configuration: bindings.RoutingConfiguration.ConfigurationService, Repository: frontend.State()}
	bindings.BackendConfiguration = feishuapp.BuildBackendConfiguration(feishuapp.BackendConfigurationInputs{
		Config: frontend.Config(), ConfigMu: frontend.ConfigMu(), Backend: scope.RuntimeOwner.Backend,
		FrontendConfigIndex: frontendConfigIndex, Store: store,
		WorkspaceSelection: bindings.WorkspaceSelection, Driver: appbackend.SelectedDriver{Selected: configuredBackend}, ModelCommands: bindings.ModelCommands,
	})
	bindings.BackendActions = feishuapp.BuildBackendActions(frontend)
	bindings.Skills = compositionkit.NewSkillService(feishuapp.SkillUseCasePorts(frontend.Config(), frontend.ConfigMu(), frontend.Context, frontend.State(), scope.RuntimeOwner.PendingSkills, frontend.FrontendID(), scope.RuntimeOwner))
	bindings.SkillCommands = feishuapp.BuildSkillCommands(feishuapp.SkillCommandInputs{
		Service: bindings.Skills, FrontendID: frontend.FrontendID(), EffectRunner: *scope.RuntimeOwner.EffectRunner,
		Actors:   scope.RuntimeOwner.SessionActors,
		RunAsync: func(fn func()) bool { return scope.RuntimeOwner.Lifecycle.Run(fn, asyncRunner) },
	})
	*bindings.PendingQueue = submission.NewPendingQueueService(feishuapp.PendingQueuePorts(frontend.Context, frontend.State(), bindings.SubmissionCleanup, frontend.Config(), frontend.ConfigMu(), frontend.Feishu()))
	bindings.Continuation.Deps = feishuapp.ContinuationPorts(frontend.Config(), frontend.ConfigMu(), frontend.Context, frontend.State(), scope.RuntimeOwner, bindings.Submissions, frontend.FrontendID(), frontendConfigIndex, frontend.Feishu())
	bindings.Compaction.Deps = feishuapp.CompactionPorts(frontend.Context, frontend.State(), scope.RuntimeOwner, frontend.FrontendID(), frontend.Feishu() != nil)
	bindings.GoalContinuation.Deps = goal.Dependencies{
		Context: scope.RuntimeOwner.Lifecycle.Context, Repository: frontend.State(), Tracker: bindings.Goals,
		Presenter: feishuapp.GoalContinuationPresenter(feishuapp.GoalCommandOutbound(identity.FrontendID(frontend.FrontendID()), effectRunner)),
		Bindings:  scope.RuntimeOwner.TurnBindings, Replies: bindings.Continuation, Streams: bindings.TurnPresentation,
		Live:               feishuapp.GoalContinuationLiveThreads(scope.RuntimeOwner.LiveThreads, frontend.State(), bindings.AnnouncementQuery, scope.RuntimeOwner.Announcements),
		DefaultWorkspaceID: feishuapp.DefaultWorkspaceID(frontend.Config(), frontend.ConfigMu()),
		BelongsToFrontend:  func(key string) bool { return feishuapp.FrontendSessionBelongsToFrontend(frontend.FrontendID(), key) },
	}
	*bindings.GoalCommands = goalcmd.NewService(goalcmd.Dependencies{
		StateProvider:  frontend.State(),
		Outbound:       feishuapp.GoalCommandOutbound(identity.FrontendID(frontend.FrontendID()), effectRunner),
		CardRenderer:   frontend.Feishu(),
		GoalManagement: bindings.GoalManagement,
		GoalTracker:    bindings.Goals,
		MakeSessionKeyFn: func(msg *feishu.InboundMessage) string {
			return feishuapp.GoalCommandSessionKey(frontend.FrontendID(), msg)
		},
		ReplyInThreadEnabledFn: func(string) bool { return false },
		MenuCardBodyForSessionFn: func(_, action, body string) string {
			return appmenuutil.MenuCardBody(action, body)
		},
		ActionStringValueFn: feishuapp.GoalCommandActionStringValue,
		ActionSessionKeyFn:  feishuapp.GoalCommandActionSessionKey,
		CompleteMenuCommandFn: func(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
			return feishuapp.CompleteGoalMenuCommand(frontend, action, sessionKey, rawCommand, parentAction)
		},
		ContextFn: scope.RuntimeOwner.Lifecycle.Context,
	})
	bindings.Interactions.Deps = feishuapp.InteractionPorts(frontend.State(), bindings.SubmissionLookup)
	bindings.InteractionDelivery = &interaction.DeliveryService{Repository: frontend.State()}
	reviewCards := feishuapp.NewOutboundCardService(feishuapp.OutboundCardInputs{
		RuntimeDeps: frontend.BackendRuntimeDeps(), Feishu: frontend.Feishu(), AsyncRunner: asyncRunner,
		InteractionDelivery: bindings.InteractionDelivery, TurnPresentation: bindings.TurnPresentation,
		Continuation: bindings.Continuation, FinalCardPatch: bindings.FinalCardPatch,
		TurnFinalFooter: bindings.TurnMetadata.TurnFinalFooterLines,
	})
	bindings.OutboundCards = reviewCards
	review := reviewapp.NewService(feishuapp.ReviewPorts(feishuapp.ReviewPortInputs{
		Runtime: frontend.BackendRuntimeDeps(), Forms: bindings.Forms, Delivery: bindings.InteractionDelivery,
		Context: frontend.Context, Repository: frontend.State(), Submissions: bindings.Submissions,
		PendingQueue: bindings.PendingQueue, Cards: reviewCards,
	}))
	bindings.Review = &review
	queuedNotice, expirePlan := feishuapp.SubmissionNoticePorts(feishuapp.SubmissionNoticeInputs{
		Config: frontend.Config(), ConfigMu: frontend.ConfigMu(), FrontendID: frontend.FrontendID(),
		FrontendConfigIndex: frontendConfigIndex, State: frontend.State(), RuntimeOwner: scope.RuntimeOwner,
		Continuation: bindings.Continuation, Feishu: frontend.Feishu(), Runner: *scope.RuntimeOwner.EffectRunner,
	})
	*bindings.Submissions = submission.NewSubmissionQueueService(feishuapp.SubmissionPorts(feishuapp.SubmissionPortInputs{
		Plan: bindings.Plan, TurnPresentation: bindings.TurnPresentation, LiveThreads: liveThreads,
		Config: frontend.Config(), ConfigMu: frontend.ConfigMu(), FrontendID: frontend.FrontendID(),
		FrontendConfigIndex: frontendConfigIndex, State: frontend.State(), Context: frontend.Context,
		Feishu: frontend.Feishu(), RuntimeOwner: scope.RuntimeOwner, RuntimeDeps: frontend.BackendRuntimeDeps(),
		AsyncRunner: asyncRunner, PendingQueue: bindings.Continuation, SkillResolver: bindings.Skills,
		Continuation: bindings.Continuation, TurnItems: bindings.TurnItems, RuntimeMaintenance: bindings.SubmissionCleanup,
		AutoRetry: bindings.AutoRetry, WorkspaceSelection: bindings.WorkspaceSelection,
		ModelSettings: bindings.ModelSnapshots, ConversationConfiguration: bindings.ConversationConfiguration,
		Starts: scope.RuntimeOwner.SubmissionStarts, QueuedNotice: queuedNotice, ExpirePlan: expirePlan,
		ReplyText:       feishuapp.SubmissionReplyTextPort(frontend.FrontendID(), *scope.RuntimeOwner.EffectRunner),
		MarkQueued:      bindings.PendingQueue.MarkSubmissionQueuedReactions,
		MarkRunning:     bindings.PendingQueue.MarkSubmissionRunningReactions,
		ClearProcessing: bindings.PendingQueue.ClearSubmissionProcessingReactions,
		StartTurn:       bindings.TurnStarter.Start, StartReview: bindings.Review.StartSubmission,
	}))
	// Turn completion callbacks run after composition, but the plan-mode service
	// and conversation service are assembled later in this function.
	turnPlanMode := &planmode.Dependencies{}
	runtimeDeps := frontend.BackendRuntimeDeps()
	*bindings.Turns = turn.NewService(feishuapp.TurnPorts(feishuapp.TurnPortInputs{
		Runtime: runtimeDeps, TurnPresentation: bindings.TurnPresentation, Cards: bindings.OutboundCards,
		TurnMetadata: bindings.TurnMetadata, Continuation: bindings.Continuation,
		PendingQueue: bindings.PendingQueue, Submissions: bindings.Submissions, AutoRetry: bindings.AutoRetry,
		SubmissionCleanup: bindings.SubmissionCleanup, Compaction: bindings.Compaction,
		GoalContinuation: bindings.GoalContinuation, PlanMode: turnPlanMode,
		AnnouncementQuery: bindings.AnnouncementQuery, AsyncRunner: asyncRunner,
	}))
	*bindings.TurnPresentation = turnstream.NewService(feishuapp.TurnPresentationPorts(feishuapp.TurnPresentationPortInputs{
		Runtime: runtimeDeps, Turns: bindings.Turns, TurnPresentation: bindings.TurnPresentation,
		Tracker: bindings.TurnStreams, Finder: bindings.SubmissionLookup, Items: bindings.TurnItems,
		Compaction: bindings.Compaction, SubmissionStatus: bindings.SubmissionStatus, Cards: bindings.OutboundCards,
	}))
	pendingCards := feishuapp.NewPendingCardDeliveryService(feishuapp.PendingCardDeliveryInputs{
		Interactions: bindings.InteractionDelivery, Lifecycle: &scope.RuntimeOwner.Lifecycle,
		Frontend: identity.FrontendID(frontend.FrontendID()), Deduper: scope.RuntimeOwner.EffectDeduper,
		Turns: bindings.TurnPresentation, Runner: *scope.RuntimeOwner.EffectRunner, Ready: frontend.Feishu() != nil,
	})
	bindings.ServerRequests = feishuapp.BuildServerRequests(feishuapp.ServerRequestInputs{
		State: frontend.State(), Feishu: frontend.Feishu(), PendingReplies: bindings.PendingReplies,
		SubmissionLookup: bindings.SubmissionLookup, PendingCards: pendingCards, EffectRunner: *scope.RuntimeOwner.EffectRunner,
		FrontendID: identity.FrontendID(frontend.FrontendID()), ConfiguredBackend: configuredBackend,
		RuntimeOwner: scope.RuntimeOwner, WorkspaceConfigured: true,
	})
	bindings.ClaudeSupport = feishuapp.BuildClaudeSupport(feishuapp.ClaudeSupportInputs{
		State: frontend.State(), Feishu: frontend.Feishu(), PendingReplies: bindings.PendingReplies,
		PendingCards: pendingCards, EffectRunner: *scope.RuntimeOwner.EffectRunner,
		FrontendID: identity.FrontendID(frontend.FrontendID()), ClaudeCore: scope.RuntimeOwner.ClaudeCore,
		WorkspaceConfigured: true,
		CancelPending: func(pending *state.PendingRequest) error {
			return bindings.ServerRequests.AdapterForPending(pending).CancelPending(pending)
		},
	})
	bindings.TurnReconciliation = turn.Reconciliation{Gateway: feishuapp.TurnReconciliationGateway(frontend.BackendRuntimeDeps()), Session: frontend.State().Session, SawFinal: bindings.TurnPresentation.StreamSawFinal, Finish: bindings.Turns.FinishTurn, Context: frontend.Context}
	bindings.ClaudeReconciliation = turn.StoppedReconciliation{Stopped: feishuapp.ClaudeSessionStopped(feishuapp.ConfiguredBackendBuilder(frontend.Config(), frontend.ConfigMu(), scope.RuntimeOwner.Backend, frontend.FrontendID(), frontendConfigIndex), scope.RuntimeOwner.ClaudeCore), Session: frontend.State().Session, Finish: bindings.Turns.FinishTurn}
	workspaceRepository := configadapter.NewWorkspaceRepository(configSource)
	bindings.BackendEvents.Deps = backendevents.Dependencies{
		Lifecycle: bindings.Turns, Items: bindings.TurnItems, Presentation: bindings.TurnPresentation,
		Compaction: bindings.Compaction, Submissions: bindings.SubmissionStatus,
		Usage: scope.RuntimeOwner.TurnBindings, Goals: bindings.Goals, Interactions: bindings.Interactions,
		InteractionPresenter: feishuapp.BackendInteractionPresenter(feishuapp.BackendInteractionPresenterPorts{
			FindSubmissionByTurn:      bindings.SubmissionLookup.FindSubmissionByTurn,
			MergeApprovalPresentation: bindings.ItemContext.MergePresentation,
			WorkspaceCwd: func(workspaceID string) string {
				workspace, err := workspaceRepository.Get(workspaceID)
				if err != nil || workspace == nil {
					return ""
				}
				return workspace.Cwd
			},
			SendApprovalCardPresentation: bindings.ServerRequests.SendApprovalCardPresentation,
			SendUserInputCard:            bindings.ServerRequests.SendUserInputCard,
			SendUserInputFormCard:        bindings.ServerRequests.SendUserInputFormCard,
			SendElicitationURLCard:       bindings.ServerRequests.SendElicitationURLCard,
			SendElicitationFormCard:      bindings.ServerRequests.SendElicitationFormCard,
			ReplyCodexError:              feishuapp.CodexErrorReplyPort(scope.RuntimeOwner),
		}),
	}
	failureCards := feishuapp.NewOutboundCardService(feishuapp.OutboundCardInputs{
		RuntimeDeps: runtimeDeps, Feishu: frontend.Feishu(), AsyncRunner: asyncRunner,
		InteractionDelivery: bindings.InteractionDelivery, TurnPresentation: bindings.TurnPresentation,
		Continuation: bindings.Continuation, FinalCardPatch: bindings.FinalCardPatch,
		TurnFinalFooter: bindings.TurnMetadata.TurnFinalFooterLines,
	})
	failure := backendfailure.NewBackendFailureService(feishuapp.BackendFailurePorts(feishuapp.BackendFailurePortInputs{
		Runtime: runtimeDeps, TurnPresentation: bindings.TurnPresentation, Compaction: bindings.Compaction,
		InteractionLifecycle: bindings.InteractionLifecycle, AutoRetry: bindings.AutoRetry,
		SubmissionCleanup: bindings.SubmissionCleanup, PendingQueue: bindings.PendingQueue,
		Submissions: bindings.Submissions, Cards: failureCards, AsyncRunner: asyncRunner,
	}))
	bindings.BackendFailure = &failure
	backendFailureOwner = bindings.BackendFailure
	// Inbound and ForwardInputs call each other at runtime, so neither can be
	// built from the other's finished value. They are wired here instead: the
	// two entry points are passed in after both exist.
	inboundService := &inbound.Service{}
	forwardService := inbound.ForwardService{Gateway: feishuapp.ForwardGateway(frontend.Feishu()), Tasks: feishuapp.ForwardTasks(&scope.RuntimeOwner.Lifecycle, asyncRunner), Context: frontend.Context, Process: feishuapp.ForwardProcessor(scope.RuntimeOwner.SessionActors, feishuapp.SessionKeyBuilder(frontend.FrontendID()), func(msg *application.InboundMessage) error { return inboundService.ProcessMessage(msg) }), Queued: bindings.PendingQueue.MarkMessagesQueuedReactions, Clear: bindings.PendingQueue.ClearMessageProcessingReactions, Failed: feishuapp.ForwardFailure(scope.RuntimeOwner.Lifecycle.Context, frontend.FrontendID(), *scope.RuntimeOwner.EffectRunner)}
	bindings.ForwardInputs = forwardService
	bindings.Conversations = &conversation.Service{Deps: feishuapp.ConversationPorts(feishuapp.ConversationPortInputs{
		Config: frontend.Config(), ConfigMu: frontend.ConfigMu(), FrontendID: frontend.FrontendID(),
		FrontendConfigIndex: frontendConfigIndex, Context: frontend.Context,
		Repository: frontend.State(), RuntimeOwner: scope.RuntimeOwner, LiveThreads: liveThreads,
		ModelSettings: bindings.ModelSnapshots,
		ThreadBinding: conversation.ThreadBindingDependencies{
			Lookup: bindings.SubmissionLookup, Bindings: scope.RuntimeOwner.TurnBindings, Replies: bindings.Continuation,
		},
		ConversationConfiguration: bindings.ConversationConfiguration,
		ContinueClaude:            bindings.Continuation.ContinueClaudeSessionWithText,
	})}
	// The workspace command services read the workspace card presentation and
	// the conversation service at construction, so they are built once those
	// bindings exist.
	bindings.WorkspaceConfiguration = feishuapp.BuildWorkspaceConfiguration(frontend, bindings.WorkspacePresentation, bindings.Conversations)
	bindings.WorkspaceManagement = feishuapp.BuildWorkspaceManagement(frontend, bindings.WorkspacePresentation, bindings.Conversations, bindingScope)
	bindings.Upgrades = feishuapp.BuildUpgrades(feishuapp.UpgradeInputs{
		Context: scope.RuntimeOwner.Lifecycle.Context, Config: scope.Config, ConfigMu: scope.ConfigMutex,
		ConfiguredBackend: configuredBackend, FrontendID: scope.Frontend.ID, FrontendConfigIndex: frontendConfigIndex,
		State: frontend.State(), Feishu: frontend.Feishu(), EffectRunner: *scope.RuntimeOwner.EffectRunner,
		WorkspacePresentation: bindings.WorkspacePresentation, WorkspaceConfiguration: bindings.WorkspaceConfiguration,
		Workflow: bindings.UpgradeWorkflow,
	})
	actors, replayRunner := feishuapp.BindingReplayPorts(scope.RuntimeOwner.SessionActors, scope.RuntimeOwner)
	bindings.BindingReplay = runtime.BindingReplay{Service: bindings.BindingPending, Runner: replayRunner, Actors: actors}
	bindings.WorkspaceEffects = workspaceapp.EffectService{Lifecycle: bindings.WorkspaceCreation.Lifecycle, Runtime: feishuapp.WorkspaceEffectRuntime(&scope.RuntimeOwner.Lifecycle, asyncRunner, scope.RuntimeOwner.SessionActors, scope.RuntimeOwner.LiveThreads, bindings.BindingReplay), Conversations: bindings.Conversations, Context: frontend.Context}
	bindings.WorkspaceWorkflow.Effects = bindings.WorkspaceEffects
	bindings.GroupWorkspaces = workspaceapp.GroupService{Frontend: identity.FrontendID(frontend.FrontendID()), Repository: frontend.State(), Creation: bindings.WorkspaceCreation, Planning: bindings.WorkspacePlanning, Effects: bindings.WorkspaceEffects}
	feishuClient := frontend.Feishu()
	bindings.BindingCommands = feishuapp.BuildBindingCommands(feishuapp.BindingCommandInputs{
		Scope: bindingScope, Config: frontend.Config(), ConfigMu: frontend.ConfigMu(), State: frontend.State(),
		FrontendID: frontend.FrontendID(), ConfiguredBackend: feishuapp.ConfiguredBackendBuilder(frontend.Config(), frontend.ConfigMu(), scope.RuntimeOwner.Backend, frontend.FrontendID(), frontendConfigIndex),
		MakeSessionKey: feishuapp.SessionKeyBuilder(frontend.FrontendID()), Context: frontend.Context,
		Effects:               *scope.RuntimeOwner.EffectRunner,
		RunAsync:              func(fn func()) { scope.RuntimeOwner.Lifecycle.Run(fn, asyncRunner) },
		RefreshGroupStatus:    scope.RuntimeOwner.Announcements.Schedule,
		CurrentBotDisplayName: feishuapp.BotDisplayName(feishuClient), CurrentLiveBotOpenID: feishuapp.LiveBotOpenID(feishuClient),
		InitializeGroupPrimary: func(ctx context.Context, chatType, chatID string) error {
			return feishuapp.EnsureGroupPrimary(ctx, bindings.PrimaryInitialization, frontend.FrontendID(), feishuClient, chatType, chatID)
		},
		SimpleStatusCard: func(title, color, body string, buttons []feishu.Button) map[string]any {
			if feishuClient == nil {
				return nil
			}
			return feishuClient.SimpleStatusCard(title, color, body, buttons)
		},
		BackendConfiguration: bindings.BackendConfiguration, Forms: bindings.Forms, FrontendQuery: bindings.FrontendQuery,
		GroupWorkspaces: bindings.GroupWorkspaces, ModelCommands: bindings.ModelCommands, ModelSnapshots: bindings.ModelSnapshots,
		Primary: bindings.Primary, RoutingConfiguration: bindings.RoutingConfiguration, ScopedRoutingConfiguration: bindings.ScopedRoutingConfiguration,
		ServiceTier: bindings.ServiceTier, WorkspaceConfiguration: bindings.WorkspaceConfiguration,
		WorkspaceManagement: bindings.WorkspaceManagement, WorkspacePresentation: bindings.WorkspacePresentation, WorkspaceWorkflow: bindings.WorkspaceWorkflow,
	})
	bindings.ConversationRecovery = conversation.NewRecovery(feishuapp.ConversationRecoveryPorts(
		frontend.Config(), frontend.ConfigMu(), frontendConfigIndex, frontend.State(),
		bindings.Conversations, scope.RuntimeOwner, bindings.CodexRecovery, bindings.ConversationConfiguration,
	))
	bindings.StartupRecovery = maintenance.NewStartupRecovery(feishuapp.StartupRecoveryPorts(feishuapp.StartupRecoveryPortInputs{
		Runtime: frontend.BackendRuntimeDeps(), StartupState: bindings.StartupState,
		CleanupExpiredAttachments: func() { bindings.MaintenanceCommands.CleanupExpiredAttachments() },
		RestoreConversationState:  bindings.ConversationRecovery.Restore,
		SendText:                  feishuapp.FrontendTextSender(frontend.FrontendID(), *scope.RuntimeOwner.EffectRunner),
	}))
	recoverFrontendRuntimeState = bindings.StartupRecovery.RecoverFrontendRuntimeState
	backendSwitch := backendselection.NewService(feishuapp.BackendSwitchPorts(feishuapp.BackendSwitchPortInputs{
		RuntimeDeps: frontend.BackendRuntimeDeps(), Transition: &scope.RuntimeOwner.BackendTransition,
		FrontendQuery: bindings.FrontendQuery, StartupRecovery: bindings.StartupRecovery,
		Announcements: scope.RuntimeOwner.Announcements, AnnouncementQuery: bindings.AnnouncementQuery,
	}))
	bindings.BackendSwitch = &backendSwitch
	bindings.BackendSelection = feishuapp.BuildBackendSelection(feishuapp.BackendSelectionInputs{
		RuntimeDeps: frontend.BackendRuntimeDeps(), RuntimeOwner: scope.RuntimeOwner, AsyncRunner: asyncRunner,
		State: frontend.State(), Feishu: frontend.Feishu(), WorkspaceSelection: bindings.WorkspaceSelection,
		UseCase: bindings.BackendSwitch, AnnouncementQuery: bindings.AnnouncementQuery,
		StartupRecovery: bindings.StartupRecovery, AutoRetry: bindings.AutoRetry, FrontendQuery: bindings.FrontendQuery,
	})
	inboundFrontendID := frontend.FrontendID()
	inboundRunner := *scope.RuntimeOwner.EffectRunner
	inboundBackend := feishuapp.ConfiguredBackendBuilder(
		frontend.Config(), frontend.ConfigMu(), scope.RuntimeOwner.Backend,
		inboundFrontendID, frontendConfigIndex,
	)
	inboundSessionKey := feishuapp.SessionKeyBuilder(inboundFrontendID)
	inboundService.Deps = feishuapp.InboundPorts(feishuapp.InboundPortInputs{
		FrontendID: inboundFrontendID, Context: frontend.Context, SessionKey: inboundSessionKey,
		Feishu: frontend.Feishu(), Primary: bindings.Primary, PrimaryInitialization: bindings.PrimaryInitialization,
		GroupMessages: bindings.GroupMessages, Requests: bindings.ServerRequests,
		InteractionLifecycle:  bindings.InteractionLifecycle,
		CompleteWorkspaceText: bindings.WorkspaceManagement.CompleteWorkspaceNewText,
		ClaudeSupport:         bindings.ClaudeSupport, Continuation: bindings.Continuation,
		PendingQueue: bindings.PendingQueue, Config: frontend.Config(), BindingPending: bindings.BindingPending,
		StoreReady: store != nil, StateReady: frontend.State() != nil,
		GateContext: scope.RuntimeOwner.Lifecycle.Context, Effects: inboundRunner,
		WorkspaceMenu: bindings.WorkspacePresentation.RenderWorkspaceMenuCard,
		LocalBackend:  inboundBackend,
		HandleCommand: func(msg *application.InboundMessage, text string) error {
			return feishuapp.HandleInboundCommand(frontend, msg, text)
		},
		SelectBackend: bindings.BackendSelection.ReplyBackendSelectionCard,
		BlockedReason: scope.RuntimeOwner.BackendTransition.BackendSwitchBlockedReasonForTraffic,
		RuntimeDeps:   frontend.BackendRuntimeDeps(), Queue: bindings.Submissions,
		RefreshGroup: func(chatID, _ string) {
			feishuapp.ScheduleGroupAnnouncementStatusRefresh(scope.RuntimeOwner.Announcements, chatID)
		},
		FlushNotifications: func(msg *application.InboundMessage) {
			if msg != nil && frontend.Feishu() != nil && store != nil {
				bindings.Notifications.Flush(msg.ChatID, msg.UserID)
			}
		},
		PrefetchForward: forwardService.Start,
	})
	bindings.Inbound = inboundService
	controls := conversation.NewControls(feishuapp.ConversationControlPorts(feishuapp.ConversationControlInputs{
		Repository: frontend.State(), Config: frontend.Config(), ConfigMu: frontend.ConfigMu(),
		FrontendID: frontend.FrontendID(), FrontendConfigIndex: frontendConfigIndex,
		Conversations: bindings.Conversations, Pending: bindings.PendingQueue,
		RetryTracker: scope.RuntimeOwner.AutoRetries, AutoRetry: bindings.AutoRetry,
		Runtime: frontend.BackendRuntimeDeps(), Context: frontend.Context,
	}))
	bindings.ConversationControls = &controls
	bindings.ThreadMenu = threadmenu.NewService(feishuapp.BuildThreadMenuDependencies(feishuapp.ThreadMenuInputs{
		Runtime: frontend.BackendRuntimeDeps(), Store: store, State: frontend.State(),
		Conversations: bindings.Conversations, BindingScope: bindingScope, ConversationQuery: bindings.ConversationQuery,
		PendingQueue: bindings.PendingQueue, WorkspaceConfiguration: bindings.WorkspaceConfiguration,
		WorkspaceSelection: bindings.WorkspaceSelection, ConversationControls: bindings.ConversationControls,
		ThreadSettings: bindings.ThreadSettings, PermissionSettings: bindings.PermissionSettings,
		BackendActions: bindings.BackendActions, AutoRetry: bindings.AutoRetry,
		CompleteMenuCommand: frontend.CompleteMenuCommand,
	}))
	planSource, planCatalog, planWorkspaces := feishuapp.PlanPorts(frontend.Config(), frontend.ConfigMu(), bindings.ModelSnapshots, scope.RuntimeOwner)
	*bindings.Plan = planapp.Service{Forms: bindings.Forms, Delivery: bindings.InteractionDelivery, Repository: frontend.State(), Settings: planapp.SettingsService{Source: planSource, Catalog: planCatalog, Context: frontend.Context}, Conversations: bindings.Conversations, Workspaces: planWorkspaces, Queue: bindings.Submissions}
	*turnPlanMode = feishuapp.PlanModePorts(feishuapp.PlanModePortInputs{
		Runtime: frontend.BackendRuntimeDeps(), UseCase: bindings.Plan, Continuation: bindings.Continuation,
		State: frontend.State(), ModelSnapshots: bindings.ModelSnapshots,
		WorkspaceSelection: bindings.WorkspaceSelection, Submissions: bindings.Submissions,
		Conversations: bindings.Conversations, Feishu: frontend.Feishu(), AsyncRunner: asyncRunner,
	})
	bindings.ReviewCommands = appreviewcmd.NewReviewFormService(feishuapp.ReviewCommandDependencies(frontend))
	bindings.MCP, err = feishuapp.BuildMCP(feishuapp.MCPPorts(feishuapp.MCPPortInputs{
		AttachmentSender: frontend.Feishu(), StateProvider: frontend.State(),
		TurnItems: bindings.TurnItems, SubmissionLookup: bindings.SubmissionLookup,
	}))
	if err != nil {
		return nil, err
	}
	scope.RuntimeOwner.MCP = runtime.NewResource(bindings.MCP)
	cardActionFrontendID := frontend.FrontendID()
	normalizeCardActionSessionKey := func(key string) string {
		return identity.CanonicalSessionKey(cardActionFrontendID, key)
	}
	bindings.CardActions = cardaction.NewService(feishuapp.CardActionPorts(
		frontend, normalizeCardActionSessionKey,
		scope.RuntimeOwner.BackendTransition.BackendSwitchBlocksCardAction,
		feishuapp.WorkspaceCardActionInputs{
			BindingCommands: bindings.BindingCommands, WorkspaceManagement: bindings.WorkspaceManagement,
			WorkspaceConfiguration: bindings.WorkspaceConfiguration, ThreadMenu: bindings.ThreadMenu,
			CompleteMenuCommand: frontend.CompleteMenuCommand,
		},
		bindings.WorkspaceConfiguration.WorkspaceDeleteActions(),
		bindings.History,
		bindings.ServerRequests, bindings.ClaudeSupport, bindings.ReviewCommands,
		feishuapp.MaintenanceCardActionInputs{
			Upgrades: bindings.Upgrades, BackendUpgrades: bindings.BackendUpgrades,
			BackendActions: bindings.BackendActions,
		}, feishuapp.SystemCardActionInputs{
			Debug: bindings.Debug, BackendUpgrades: bindings.BackendUpgrades,
			BackendActions: bindings.BackendActions, CompleteMenuCommand: frontend.CompleteMenuCommand,
		}, feishuapp.PathPickerActionInputs{
			State: frontend.State(), Forms: bindings.Forms, Picker: bindings.PathPicker,
			Planning: bindings.WorkspacePlanning, WorkspaceCards: bindings.WorkspacePresentation,
			Upgrades: bindings.Upgrades, Debug: bindings.Debug,
			SimpleStatusCard: frontend.Feishu().SimpleStatusCard,
		}, bindings.ThreadMenu, *turnPlanMode, feishuapp.AsyncUserInputActionInputs{
			State: frontend.State(), Inputs: bindings.AsyncInputs, Context: frontend.Context,
			FrontendID: frontend.FrontendID(), EffectRunner: *scope.RuntimeOwner.EffectRunner,
			SimpleStatusCard: frontend.Feishu().SimpleStatusCard,
		}, feishuapp.PendingFormCancelActionInputs{
			State: frontend.State(), ServerRequests: bindings.ServerRequests,
			FinalizePending:     bindings.PendingReplies.Finalize,
			WorkspaceMenuCard:   bindings.WorkspacePresentation.RenderWorkspaceMenuCard,
			SimpleStatusCard:    frontend.Feishu().SimpleStatusCard,
			WorkspaceConfigured: frontend.Config() != nil,
		},
	))
	dispatcher := feishuapp.NewDispatcher(feishuapp.DispatcherInputs{
		FrontendID: frontend.FrontendID(), Started: scope.StartedAt, Inbound: bindings.Inbound,
		CardActions: bindings.CardActions, BackendEvents: bindings.BackendEvents, AutoRetry: bindings.AutoRetry,
		RuntimeOwner: scope.RuntimeOwner, EffectRunner: *scope.RuntimeOwner.EffectRunner, ReplyInThread: false,
	})
	scope.RuntimeOwner.Dispatcher = &dispatcher
	*autoRetryRuntimeDeps = frontend.BackendRuntimeDeps()
	if err := feishuapp.CanonicalizeStoredSessionKeys(store); err != nil {
		return nil, err
	}
	if backend := configuredBackend(); backend != "" {
		handle, err := feishuapp.BuildBackendRuntimeHandle(frontend.BackendRuntimeDeps(), backend)
		if err != nil {
			return nil, err
		}
		feishuapp.InstallBackendRuntime(frontend.BackendRuntimeDeps(), handle)
	}
	deps := frontend.BackendRuntimeDeps()
	*claudeRuntimeInputs = feishuapp.ClaudeRuntimePortInputs{
		Runtime: deps,
		Cards: feishuapp.NewOutboundCardService(feishuapp.OutboundCardInputs{
			RuntimeDeps: deps, Feishu: frontend.Feishu(), AsyncRunner: asyncRunner,
			InteractionDelivery: bindings.InteractionDelivery, TurnPresentation: bindings.TurnPresentation,
			Continuation: bindings.Continuation, FinalCardPatch: bindings.FinalCardPatch,
			TurnFinalFooter: bindings.TurnMetadata.TurnFinalFooterLines,
		}),
		SubmissionLookup: bindings.SubmissionLookup, ModelSnapshots: bindings.ModelSnapshots,
		ModelAcknowledgements: bindings.ModelAcknowledgements, ClaudeSupport: bindings.ClaudeSupport,
		InteractionLifecycle: bindings.InteractionLifecycle, ItemContext: bindings.ItemContext,
		TurnPresentation: bindings.TurnPresentation, Turns: bindings.Turns,
		BackendFailure: bindings.BackendFailure, Usage: bindings.Usage,
		TurnMetadata: bindings.TurnMetadata, ConversationQuery: bindings.ConversationQuery,
		Conversations: bindings.Conversations,
	}
	feishuapp.InstallFeishuPolicies(feishuapp.FeishuPolicyInputs{
		Client: frontend.Feishu(), GroupMessages: bindings.GroupMessages, Context: frontend.Context,
		PrimaryInitialization: bindings.PrimaryInitialization, FrontendID: frontend.FrontendID(),
		Announcements: bindings.Announcements, AnnouncementRefresh: scope.RuntimeOwner.Announcements,
	})
	transport, ok := scope.FeishuTransport.(app.HandlerSet)
	if !ok {
		return nil, fmt.Errorf("frontend composition has no Feishu handler transport")
	}
	app.BindHandlers(transport, frontend)
	return frontend, nil
}

func scopes(cfg *config.Config, cfgPath string) ([]FrontendScope, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	frontends := cfg.ResolvedFrontends()
	if len(frontends) == 0 {
		return nil, fmt.Errorf("no frontend configured")
	}
	store, err := state.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	mu := &sync.RWMutex{}
	result := make([]FrontendScope, 0, len(frontends))
	for _, frontend := range frontends {
		transport := appfeishuwrap.WrapFeishuClient(feishu.New(frontend.Feishu))
		result = append(result, FrontendScope{
			Config: cfg, ConfigPath: cfgPath, Store: store, ConfigMutex: mu, Frontend: frontend,
			FeishuTransport: transport,
			RuntimeOwner:    runtime.NewFrontendOwner(),
		})
	}
	return result, nil
}

func NewService[T runtime.ManagedFrontend](cfg *config.Config, cfgPath string, factory Factory[T]) (*Service[T], error) {
	inputs, err := scopes(cfg, cfgPath)
	if err != nil {
		return nil, err
	}
	service := &Service[T]{}
	for _, input := range inputs {
		frontend, err := factory(input)
		if err != nil {
			return nil, err
		}
		service.Frontends = append(service.Frontends, frontend)
		service.FrontendGroup.Frontends = append(service.FrontendGroup.Frontends, frontend)
	}
	return service, nil
}

func New[T runtime.ManagedFrontend](cfg *config.Config, cfgPath string, factory Factory[T]) (T, error) {
	var zero T
	inputs, err := scopes(cfg, cfgPath)
	if err != nil {
		return zero, err
	}
	return factory(inputs[0])
}
