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
	frontend, err := feishuapp.NewFeishuShell(scope)
	if err != nil {
		return nil, err
	}
	// All production objects are assembled here. internal/app only binds the
	// already composed Feishu event transport to the frontend entrypoint.
	feishuapp.AttachEffectRunner(frontend, feishuapp.NewEffectRunner(frontend))
	feishuapp.AttachStateView(frontend, feishuapp.NewStateView(frontend))
	scope.RuntimeOwner.TurnBindings = turnbinding.NewTracker(frontend.State().Submission)
	bindings := &feishuapp.Bindings{
		TurnStreams: turnstream.NewTracker(), TurnItems: turnitem.NewTracker(), FinalCardPatches: finalcardpatch.NewTracker(), Goals: goal.NewTracker(),
		Submissions: &submission.SubmissionQueueService{}, PendingQueue: &submission.PendingQueueService{},
		Continuation: &continuation.Service{}, Turns: &turn.Service{}, TurnPresentation: &turnstream.Service{},
		Compaction: &compaction.Service{}, GoalContinuation: &goal.Service{}, GoalCommands: &goalcmd.Service{},
		Interactions: &interaction.Service{}, BackendEvents: &backendevents.Service{}, Plan: &planapp.Service{},
	}
	bindings.GoalManagement = &goal.Management{Tracker: bindings.Goals, Context: frontend.Context, Gateway: func() (goal.Gateway, error) {
		return feishuapp.RequireCodexGoalGateway(frontend)
	}}
	frontend.AttachBindings(bindings)
	bindings.RuntimeSettings = runtimeconfig.Service{Repository: configadapter.NewRuntimeRepository(frontend)}
	bindings.PathPicker = pathpicker.Service{Filesystem: filesystempicker.Filesystem{}}
	bindings.AsyncInputs = asyncinput.Service{Deps: asyncinput.Dependencies{Repository: frontend.State(), Backend: func() string { return feishuapp.BackendKind(frontend) }, Context: frontend.Context, Run: feishuapp.SessionTaskRunner(frontend), Effects: feishuapp.NewEffectRunner(frontend)}}
	bindings.WorkspaceSelection = workspaceapp.SelectionService{Frontend: identity.FrontendID(frontend.FrontendID()), Repository: scoped.WorkspaceSelections{Store: frontend.State()}, DefaultWorkspaceID: feishuapp.DefaultWorkspaceID(frontend)}
	bindings.SubmissionLookup = submission.SubmissionLookupService{State: frontend.State(), Runtime: scope.RuntimeOwner.TurnBindings}
	bindings.SubmissionStatus = submission.StatusService{Lookup: bindings.SubmissionLookup, Repository: frontend.State()}
	bindings.InteractionLifecycle = interaction.LifecycleService{Repository: frontend.State(), Frontend: frontend.FrontendID(), Presentation: feishuapp.InteractionExpiryPresentation(frontend)}
	bindings.ModelAcknowledgements = modelconfig.AcknowledgementService{Repository: frontend.State()}
	bindings.ClaudeFactory = func(cfg config.ClaudeConfig) feishuapp.ClaudeCore {
		return clauderuntime.NewService(feishuapp.ClaudeRuntimePorts(frontend, cfg))
	}
	routingConfiguration := routing.ConfigurationService{Repository: frontend.State(), Frontend: identity.FrontendID(frontend.FrontendID())}
	bindings.RoutingConfiguration = compositionkit.RoutingConfiguration{ConfigurationService: routingConfiguration, Runner: feishuapp.NewEffectRunner(frontend), Context: frontend.Context()}
	bindings.ScopedRoutingConfiguration = compositionkit.ScopedRoutingConfiguration{Service: routing.ScopedConfigurationService{ConfigurationService: routingConfiguration, BackendSource: func() string { return feishuapp.BackendKind(frontend) }}, Runner: feishuapp.NewEffectRunner(frontend), Context: frontend.Context()}
	primaryRepository := statejson.NewGroupPrimaryRepository(frontend.Store(), frontend.FrontendID())
	bindings.Primary = routing.Service{Repository: primaryRepository}
	bindings.GroupMessages = routing.GroupMessages{Frontend: frontend.FrontendID(), Primary: bindings.Primary, Links: frontend.State(), SelfOpenID: feishuapp.LiveBotOpenID(frontend)}
	bindings.Announcements = announcement.Service{Repository: frontend.State(), Gateway: feishuapp.AnnouncementGateway(frontend.Feishu()), Frontend: frontend.FrontendID(), Primary: func(chatID string) bool {
		enabled, _ := bindings.Primary.IsPrimary(frontend.FrontendID(), "group", chatID)
		return enabled
	}}
	bindings.AnnouncementQuery = announcement.Query{Repository: frontend.State(), Workspaces: configadapter.NewWorkspaceRepository(frontend), HasPrimary: func(chatID string) bool {
		record, _ := bindings.Primary.Lookup(frontend.FrontendID(), "group", chatID)
		return record != nil
	}}
	scope.RuntimeOwner.Announcements = runtime.NewCoalescedRefresh(&scope.RuntimeOwner.Lifecycle, 2*time.Second, 15*time.Second, feishuapp.GroupAnnouncementRefresh(frontend))
	liveThreads := feishuapp.SubmissionLiveThreads(scope.RuntimeOwner.LiveThreads, frontend.State().Session, bindings.AnnouncementQuery, scope.RuntimeOwner.Announcements.Schedule)
	bindings.PrimaryInitialization = routing.InitializationService{Repository: primaryRepository, BotCount: frontend.Feishu().GetGroupBotCount, LiveBotOpenID: feishuapp.LiveBotOpenID(frontend)}
	bindings.TurnMetadata = turnmeta.Service{Tracker: scope.RuntimeOwner.TurnBindings}
	bindings.ItemContext = approval.ItemContext{Items: bindings.TurnItems, Started: func(threadID, turnID string) { bindings.Turns.BindPendingSubmissionTurn(threadID, turnID, true) }}
	bindings.PendingReplies = feishuapp.PendingReplyAdapter{Service: bindings.Interactions, Repository: frontend.State()}
	filesystem, git := feishuapp.WorkspaceCreationPorts(frontend.ConfigPath())
	workspaceLifecycle := &workspaceapp.Lifecycle{Frontend: identity.FrontendID(frontend.FrontendID()), Selection: frontend.WorkspaceSelection(), Configuration: workspaceapp.ConfigurationService{Repository: configadapter.NewWorkspaceRepository(frontend)}, Repository: configadapter.WorkspaceLifecycleRepository{Source: frontend, Scope: frontend.State()}}
	bindings.WorkspaceCreation = &workspaceapp.CreationService{Filesystem: filesystem, Git: git, Lifecycle: workspaceLifecycle}
	bindings.WorkspaceSettings = workspaceapp.SettingsService{Frontend: frontend.FrontendID(), Repository: configadapter.WorkspaceSettingsRepository{Source: frontend, Scope: frontend.State()}}
	bindings.WorkspacePlanning = &workspaceapp.PlanningService{Repository: configadapter.NewWorkspaceRepository(frontend), PendingRequests: frontend.State().PendingRequests, ConfigPath: frontend.ConfigPath, BotName: frontend.Feishu().BotName, FrontendID: frontend.FrontendID, Paths: runtimeworkspace.PlanningFilesystem{}, Git: runtimeworkspace.PlanningGit{}}
	bindings.Forms = &interaction.FormService{Repository: frontend.State()}
	bindings.WorkspaceWorkflow = &workspaceapp.Workflow{Forms: bindings.Forms, Planning: bindings.WorkspacePlanning, Creation: bindings.WorkspaceCreation}
	feishuapp.AttachWorkspacePresentation(frontend, feishuapp.NewWorkspacePresentation(frontend))
	bindings.ModelSnapshots = modelconfig.SnapshotService{Repository: feishuapp.ModelSnapshotRepository(frontend)}
	bindings.ModelDefaults = modelconfig.DefaultsService{Repository: configadapter.ModelDefaultsRepository{Source: frontend, Scope: frontend.State()}, Admission: feishuapp.ModelWriteAdmission(frontend), Frontend: frontend.FrontendID(), Publisher: feishuapp.ModelDefaultsPublisher(frontend)}
	bindings.ModelOptions = modelconfig.OptionsService{Repository: configadapter.ModelOptionsRepository{Source: frontend}}
	bindings.ModelCommands = feishuapp.BuildModelCommands(frontend)
	bindings.BindingCommands = feishuapp.BuildBindingCommands(frontend)
	bindings.BackendUpgrades = feishuapp.BuildBackendUpgrades(frontend)
	bindings.UpgradePresentation = feishuapp.BuildUpgradePresentation(frontend)
	platform, releases, artifacts, launcher := feishuapp.UpgradeWorkflowPorts(frontend.Config(), frontend.ConfigMu(), scope.RuntimeOwner)
	bindings.UpgradeWorkflow = &upgrade.Service{Forms: bindings.Forms, Platform: platform, Releases: releases, Artifacts: artifacts, Launcher: launcher}
	bindings.Upgrades = feishuapp.BuildUpgrades(frontend)

	bindings.Maintenance = backendmaintenance.NewMaintenanceStateService(scope.RuntimeOwner.MaintenanceTrackers, feishuapp.MaintenanceRepository(frontend))
	bindings.StartupState = conversation.StartupState{Repository: frontend.State(), DefaultWorkspaceID: func() string { return frontend.WorkspaceSelection().ResolveSession(nil) }}
	bindings.UpgradePoller = upgrade.Poller{Repository: frontend.State(), Units: upgradeunits.Units{}}
	bindings.StartupRecovery = maintenance.NewStartupRecovery(feishuapp.StartupRecoveryPorts(frontend, func() {
		bindings.MaintenanceCommands.CleanupExpiredAttachments()
	}))
	bindings.MaintenanceCommands = feishuapp.BuildMaintenanceCommands(frontend)
	bindings.SubmissionCleanup = maintenance.SubmissionCleanup{Repository: frontend.State(), Runtime: scope.RuntimeOwner.TurnBindings, Items: bindings.TurnItems}
	bindings.AutoRetry = feishuapp.AutoRetryView(frontend)
	bindings.AutoRetry.Engine = retry.NewEngine(feishuapp.AutoRetryPorts(frontend, bindings.AutoRetry, liveThreads))
	bindings.FrontendQuery = frontendapp.Query{Repository: frontend.State(), Facts: feishuapp.FrontendFacts(frontend), Retrying: bindings.AutoRetry.HasBlockingAutoRetry}
	var codexUpgrade codexruntime.UpgradeService
	bindings.CodexRecovery = codexruntime.NewRecoveryService(feishuapp.CodexRecoveryPorts(frontend,
		func(ctx context.Context) (codexruntime.CodexClient, error) {
			return codexUpgrade.StartVerifiedCodexClient(ctx)
		},
		func() { bindings.StartupRecovery.RecoverFrontendRuntimeState() },
	))
	codexUpgrade = codexruntime.NewUpgradeService(feishuapp.CodexUpgradePorts(
		frontend.Config(), frontend.ConfigMu(), frontend.FrontendID(), frontend.FrontendConfigIndex(),
		scope.RuntimeOwner, frontend.BackendRuntimeDeps(), bindings.CodexRecovery,
		func() { _ = bindings.StartupRecovery.RecoverFrontendRuntimeState() },
		func(ctx context.Context) error { return codexUpgrade.CodexSmokeTest(ctx) },
	))
	bindings.CodexUpgrade = codexUpgrade
	smoke, active, current, create := feishuapp.ClaudeMaintenancePorts(frontend.Config(), frontend.ConfigMu(), frontend.Context, scope.RuntimeOwner, frontend.FrontendConfigIndex(), bindings.ClaudeFactory)
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
		bindings.MaintenanceRunners[kind] = maintenance.OperationRunner{Lifecycle: &scope.RuntimeOwner.Lifecycle, Service: service, Executor: feishuapp.AsyncExecutor(frontend.AsyncRunner())}
	}
	frontendID := identity.FrontendID(frontend.FrontendID())
	bindings.History = feishuapp.BuildHistory(
		frontendID, frontend.State(), feishuapp.ConfiguredBackendBuilder(frontend.Config(), frontend.ConfigMu(), scope.RuntimeOwner.Backend, frontend.FrontendID(), frontend.FrontendConfigIndex()),
		func() codexadapter.RPCClient { return scope.RuntimeOwner.CodexClient() },
		scope.RuntimeOwner.Lifecycle.Context, *scope.RuntimeOwner.EffectRunner,
		feishuapp.SessionKeyBuilder(frontend.FrontendID()), func(string) bool { return false },
	)
	sharedArtifacts, downloadPresentation, downloadRunner := feishuapp.FileSharePorts(frontend)
	bindings.FileSharing = &fileshare.Service{Forms: bindings.Forms, Repository: frontend.State(), Artifacts: sharedArtifacts, Presentation: downloadPresentation, Context: frontend.Context, Run: downloadRunner}
	bindings.Debug = feishuapp.BuildDebug(frontend)
	bindings.Usage = feishuapp.BuildUsage(frontend)
	bindings.FinalCardPatch = feishuapp.BuildFinalCardPatch(frontend)
	bindings.ClaudeSupport = feishuapp.BuildClaudeSupport(frontend)
	bindings.ThreadSettings = threadsettings.Service{Repository: frontend.State()}
	bindings.PermissionSettings = threadsettings.PermissionService{Settings: bindings.ThreadSettings, Source: configadapter.ThreadPermissionRepository{Source: frontend, Scope: frontend.State()}, Runtime: feishuapp.PermissionRuntime(frontend), Tasks: feishuapp.PermissionTasks(frontend), Failure: feishuapp.PermissionFailure(frontend), Context: frontend.Context}
	bindings.ServiceTier = feishuapp.BuildServiceTier(
		bindings.ThreadSettings, frontend.Context, identity.FrontendID(frontend.FrontendID()),
		*scope.RuntimeOwner.EffectRunner, feishuapp.SessionKeyBuilder(frontend.FrontendID()),
	)
	bindings.ModelSettings = modelconfig.SettingsService{Repository: frontend.State(), Admission: feishuapp.ModelWriteAdmission(frontend), Frontend: identity.FrontendID(frontend.FrontendID())}
	bindings.ConversationConfiguration = conversation.Configuration{Models: bindings.ModelSnapshots, ServiceName: feishuapp.CodexServiceName(frontend)}
	bindings.TurnStarter = submission.TurnStarter{Frontend: identity.FrontendID(frontend.FrontendID()), Effects: feishuapp.NewEffectRunner(frontend), Collaboration: bindings.Plan}
	bindings.BindingPending = routing.PendingService{Configuration: bindings.RoutingConfiguration.ConfigurationService, Repository: frontend.State()}
	bindings.BackendConfiguration = feishuapp.BuildBackendConfiguration(frontend)
	bindings.BackendActions = feishuapp.BuildBackendActions(frontend)
	backendSwitch := backendselection.NewService(feishuapp.BackendSwitchPorts(frontend))
	bindings.BackendSwitch = &backendSwitch
	bindings.BackendSelection = feishuapp.BuildBackendSelection(frontend)
	bindings.ServerRequests = feishuapp.BuildServerRequests(frontend)
	bindings.Skills = compositionkit.NewSkillService(feishuapp.SkillUseCasePorts(frontend.Config(), frontend.ConfigMu(), frontend.Context, frontend.State(), scope.RuntimeOwner.PendingSkills, frontend.FrontendID(), scope.RuntimeOwner))
	bindings.SkillCommands = feishuapp.BuildSkillCommands(frontend)
	*bindings.PendingQueue = submission.NewPendingQueueService(feishuapp.PendingQueuePorts(frontend.Context, frontend.State(), bindings.SubmissionCleanup, frontend.Config(), frontend.ConfigMu(), frontend.Feishu()))
	bindings.Continuation.Deps = feishuapp.ContinuationPorts(frontend.Config(), frontend.ConfigMu(), frontend.Context, frontend.State(), scope.RuntimeOwner, bindings.Submissions, frontend.FrontendID(), frontend.FrontendConfigIndex(), frontend.Feishu())
	bindings.Compaction.Deps = feishuapp.CompactionPorts(frontend.Context, frontend.State(), scope.RuntimeOwner, frontend.FrontendID(), frontend.Feishu() != nil)
	bindings.GoalContinuation.Deps = feishuapp.GoalContinuationPorts(frontend)
	*bindings.GoalCommands = goalcmd.NewService(goalcmd.Dependencies{
		StateProvider:  frontend.State(),
		Outbound:       feishuapp.GoalCommandOutbound(identity.FrontendID(frontend.FrontendID()), feishuapp.NewEffectRunner(frontend)),
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
	review := reviewapp.NewService(feishuapp.ReviewPorts(frontend))
	bindings.Review = &review
	*bindings.Submissions = submission.NewSubmissionQueueService(feishuapp.SubmissionPorts(frontend, bindings.Plan, bindings.TurnPresentation, liveThreads))
	*bindings.Turns = turn.NewService(feishuapp.TurnPorts(frontend, bindings.TurnPresentation))
	*bindings.TurnPresentation = turnstream.NewService(feishuapp.TurnPresentationPorts(frontend, bindings.Turns))
	bindings.TurnReconciliation = turn.Reconciliation{Gateway: feishuapp.TurnReconciliationGateway(frontend), Session: frontend.State().Session, SawFinal: bindings.TurnPresentation.StreamSawFinal, Finish: bindings.Turns.FinishTurn, Context: frontend.Context}
	bindings.ClaudeReconciliation = turn.StoppedReconciliation{Stopped: feishuapp.ClaudeSessionStopped(frontend), Session: frontend.State().Session, Finish: bindings.Turns.FinishTurn}
	workspaceRepository := configadapter.NewWorkspaceRepository(frontend)
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
	failure := backendfailure.NewBackendFailureService(feishuapp.BackendFailurePorts(frontend))
	bindings.BackendFailure = &failure
	// Inbound and ForwardInputs call each other at runtime, so neither can be
	// built from the other's finished value. They are wired here instead: the
	// two entry points are passed in after both exist.
	inboundService := &inbound.Service{}
	forwardService := inbound.ForwardService{Gateway: feishuapp.ForwardGateway(frontend.Feishu()), Tasks: feishuapp.ForwardTasks(&scope.RuntimeOwner.Lifecycle, frontend.AsyncRunner()), Context: frontend.Context, Process: feishuapp.ForwardProcessor(frontend, func(msg *application.InboundMessage) error { return inboundService.ProcessMessage(msg) }), Queued: bindings.PendingQueue.MarkMessagesQueuedReactions, Clear: bindings.PendingQueue.ClearMessageProcessingReactions, Failed: feishuapp.ForwardFailure(frontend)}
	inboundService.Deps = feishuapp.InboundPorts(frontend, forwardService.Start)
	bindings.Inbound = inboundService
	bindings.ForwardInputs = forwardService
	bindings.Conversations = &conversation.Service{Deps: feishuapp.ConversationPorts(frontend, liveThreads)}
	// The workspace command services read the workspace card presentation and
	// the conversation service at construction, so they are built once those
	// bindings exist.
	bindings.WorkspaceConfiguration = feishuapp.BuildWorkspaceConfiguration(frontend, bindings.WorkspacePresentation, bindings.Conversations)
	bindings.WorkspaceManagement = feishuapp.BuildWorkspaceManagement(frontend, bindings.WorkspacePresentation, bindings.Conversations)
	bindings.WorkspaceEffects = workspaceapp.EffectService{Lifecycle: bindings.WorkspaceCreation.Lifecycle, Runtime: feishuapp.WorkspaceEffectRuntime(frontend), Conversations: bindings.Conversations, Context: frontend.Context}
	bindings.WorkspaceWorkflow.Effects = bindings.WorkspaceEffects
	bindings.GroupWorkspaces = workspaceapp.GroupService{Frontend: identity.FrontendID(frontend.FrontendID()), Repository: frontend.State(), Creation: bindings.WorkspaceCreation, Planning: bindings.WorkspacePlanning, Effects: bindings.WorkspaceEffects}
	actors, replayRunner := feishuapp.BindingReplayPorts(scope.RuntimeOwner.SessionActors, scope.RuntimeOwner)
	bindings.BindingReplay = runtime.BindingReplay{Service: bindings.BindingPending, Runner: replayRunner, Actors: actors}
	bindings.ConversationQuery = conversation.Query{Repository: frontend.State()}
	bindings.Notifications = frontendapp.Notifications{Repository: frontend.State(), Sender: feishuapp.NotificationSender(frontend), Context: frontend.Context}
	bindings.ConversationRecovery = conversation.NewRecovery(feishuapp.ConversationRecoveryPorts(
		frontend.Config(), frontend.ConfigMu(), frontend.FrontendConfigIndex(), frontend.State(),
		bindings.Conversations, scope.RuntimeOwner, bindings.CodexRecovery, bindings.ConversationConfiguration,
	))
	controls := conversation.NewControls(feishuapp.ConversationControlPorts(frontend))
	bindings.ConversationControls = &controls
	bindings.ThreadMenu = feishuapp.BuildThreadMenu(frontend)
	planSource, planCatalog, planWorkspaces := feishuapp.PlanPorts(frontend.Config(), frontend.ConfigMu(), bindings.ModelSnapshots, scope.RuntimeOwner)
	*bindings.Plan = planapp.Service{Forms: bindings.Forms, Delivery: bindings.InteractionDelivery, Repository: frontend.State(), Settings: planapp.SettingsService{Source: planSource, Catalog: planCatalog, Context: frontend.Context}, Conversations: bindings.Conversations, Workspaces: planWorkspaces, Queue: bindings.Submissions}
	bindings.ReviewCommands = feishuapp.BuildReviewCommands(frontend)
	bindings.MCP, err = feishuapp.BuildMCP(frontend)
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
		bindings.WorkspaceConfiguration.WorkspaceDeleteActions(),
		bindings.History,
		bindings.ServerRequests, bindings.ClaudeSupport, bindings.ReviewCommands,
		bindings.Upgrades, bindings.BackendUpgrades,
	))
	feishuapp.AttachDispatcher(frontend, feishuapp.NewDispatcher(frontend))
	if err := feishuapp.CanonicalizeStoredSessionKeys(frontend); err != nil {
		return nil, err
	}
	if backend := feishuapp.BackendKind(frontend); backend != "" {
		handle, err := feishuapp.BuildBackendRuntimeHandle(frontend, backend)
		if err != nil {
			return nil, err
		}
		feishuapp.InstallBackendRuntime(frontend, handle)
	}
	feishuapp.InstallFeishuPolicies(frontend)
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
