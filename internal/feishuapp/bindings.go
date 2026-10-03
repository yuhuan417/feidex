package feishuapp

import (
	retryview "feidex/internal/adapter/feishu/autoretry"
	claudesupport "feidex/internal/adapter/feishu/claudesupport"
	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	history "feidex/internal/adapter/feishu/history"
	appmaintenance "feidex/internal/adapter/feishu/maintenance"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	tier "feidex/internal/adapter/feishu/servicetier"
	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
	modelconfig "feidex/internal/application/modelconfig"
	routing "feidex/internal/application/routing"
	clauderuntime "feidex/internal/runtime/claude"
	appcodexruntime "feidex/internal/runtime/codex"
	maintenance "feidex/internal/runtime/maintenance"

	"feidex/internal/adapter/feishu/approval"
	"feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/finalcardpatch"
	goalcmd "feidex/internal/adapter/feishu/goalcmd"
	modelcommands "feidex/internal/adapter/feishu/modelconfig"
	"feidex/internal/adapter/feishu/serverrequest"
	skills "feidex/internal/adapter/feishu/skills"
	"feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/adapter/feishu/turnitem"
	"feidex/internal/adapter/feishu/turnmeta"
	turnstream "feidex/internal/adapter/feishu/turnstream"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	"feidex/internal/adapter/feishu/workspacecmd"
	"feidex/internal/application/announcement"
	"feidex/internal/application/asyncinput"
	"feidex/internal/application/backendevents"
	"feidex/internal/application/backendfailure"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/application/backendselection"
	"feidex/internal/application/cardaction"
	"feidex/internal/application/compaction"
	"feidex/internal/application/continuation"
	"feidex/internal/application/conversation"
	"feidex/internal/application/fileshare"
	"feidex/internal/application/frontend"
	"feidex/internal/application/goal"
	"feidex/internal/application/inbound"
	"feidex/internal/application/interaction"
	"feidex/internal/application/pathpicker"
	planapp "feidex/internal/application/plan"
	reviewapp "feidex/internal/application/review"
	"feidex/internal/application/runtimeconfig"
	"feidex/internal/application/skill"
	"feidex/internal/application/submission"
	"feidex/internal/application/threadsettings"
	"feidex/internal/application/turn"
	"feidex/internal/application/upgrade"
	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/compositionkit"
	"feidex/internal/config"
	"feidex/internal/runtime"
)

// Bindings is an explicit frontend adapter graph, injected by composition.
// It has no string lookup, cache invalidation or lazy construction.
type Bindings struct {
	AsyncInputs                asyncinput.Service
	RuntimeSettings            runtimeconfig.Service
	PathPicker                 pathpicker.Service
	Announcements              announcement.Service
	AnnouncementQuery          announcement.Query
	CardActions                cardaction.Service
	SubmissionLookup           submission.SubmissionLookupService
	SubmissionStatus           submission.StatusService
	InteractionLifecycle       interaction.LifecycleService
	ModelAcknowledgements      modelconfig.AcknowledgementService
	ClaudeFactory              func(config.ClaudeConfig) ClaudeCore
	BackendMaintenance         map[string]*backendmaintenance.Service
	MaintenanceRunners         map[string]maintenance.OperationRunner
	RoutingConfiguration       compositionkit.RoutingConfiguration
	ScopedRoutingConfiguration compositionkit.ScopedRoutingConfiguration
	Primary                    routing.Service
	GroupMessages              routing.GroupMessages
	PrimaryInitialization      routing.InitializationService
	ModelCommands              modelcommands.ModelConfigService
	BindingCommands            bindingService
	BackendUpgrades            backendUpgradeService
	UpgradePresentation        upgradeRenderService
	Upgrades                   appupgradecmd.UpgradeService
	ReviewCommands             appreviewcmd.ReviewFormService
	Maintenance                backendmaintenance.MaintenanceStateService
	MaintenanceCommands        appmaintenance.RuntimeMaintenanceService
	StartupRecovery            maintenance.StartupRecovery
	StartupState               conversation.StartupState
	UpgradePoller              upgrade.Poller
	UpgradeWorkflow            *upgrade.Service
	SubmissionCleanup          maintenance.SubmissionCleanup
	AutoRetry                  retryview.Service
	CodexUpgrade               appcodexruntime.UpgradeService
	CodexRecovery              appcodexruntime.RecoveryService
	ClaudeMaintenance          *clauderuntime.Maintenance
	History                    history.Service
	Debug                      appdebugviewcmd.DebugService
	FileSharing                *fileshare.Service
	Usage                      appdebugviewcmd.UsageService
	FinalCardPatch             finalcardpatch.Service
	ClaudeSupport              *claudesupport.Service
	ServiceTier                tier.Service
	ThreadSettings             threadsettings.Service
	PermissionSettings         threadsettings.PermissionService
	ForwardInputs              inbound.ForwardService
	ModelSnapshots             modelconfig.SnapshotService
	ModelSettings              modelconfig.SettingsService
	ModelDefaults              modelconfig.DefaultsService
	ModelOptions               modelconfig.OptionsService
	BindingPending             routing.PendingService
	BindingReplay              runtime.BindingReplay

	Plan                      *planapp.Service
	Review                    *reviewapp.Service
	TurnMetadata              turnmeta.Service
	ItemContext               approval.ItemContext
	PendingReplies            PendingReplyAdapter
	BackendSwitch             *backendselection.Service
	BackendFailure            *backendfailure.BackendFailureService
	WorkspaceCreation         *workspaceapp.CreationService
	WorkspaceSelection        workspaceapp.SelectionService
	WorkspaceSettings         workspaceapp.SettingsService
	WorkspacePlanning         *workspaceapp.PlanningService
	WorkspaceWorkflow         *workspaceapp.Workflow
	WorkspaceEffects          workspaceapp.EffectService
	GroupWorkspaces           workspaceapp.GroupService
	Forms                     *interaction.FormService
	TurnStreams               *turnstream.Tracker
	TurnItems                 *turnitem.Tracker
	FinalCardPatches          *finalcardpatch.Tracker
	Goals                     *goal.Tracker
	GoalManagement            *goal.Management
	BackendConfiguration      backend.ConfigurationService
	BackendActions            backend.ActionService
	BackendSelection          backend.SelectionService
	WorkspacePresentation     *workspacecards.Presentation
	WorkspaceConfiguration    *workspacecmd.ConfigService
	WorkspaceManagement       *workspacecmd.ManagementService
	ThreadMenu                *threadmenu.Service
	ServerRequests            *serverrequest.Service
	Submissions               *submission.SubmissionQueueService
	Conversations             *conversation.Service
	ConversationConfiguration conversation.Configuration
	TurnStarter               submission.TurnStarter
	TurnReconciliation        turn.Reconciliation
	ClaudeReconciliation      turn.StoppedReconciliation
	ConversationQuery         conversation.Query
	FrontendQuery             frontend.Query
	Notifications             frontend.Notifications
	ConversationRecovery      conversation.Recovery
	ConversationControls      *conversation.Controls
	PendingQueue              *submission.PendingQueueService
	Continuation              *continuation.Service
	Turns                     *turn.Service
	TurnPresentation          *turnstream.Service
	Compaction                *compaction.Service
	GoalContinuation          *goal.Service
	GoalCommands              *goalcmd.Service
	Interactions              *interaction.Service
	InteractionDelivery       *interaction.DeliveryService
	Inbound                   *inbound.Service
	BackendEvents             *backendevents.Service
	Skills                    *skill.Service
	SkillCommands             *skills.Service
	MCP                       *feidexMCPService
}

func (a *App) AttachBindings(bindings *Bindings) { a.bindings = bindings }
func BuildBackendConfiguration(a *App) backend.ConfigurationService {
	return buildBackendConfigurationService(a)
}
func BuildBackendActions(a *App) backend.ActionService      { return buildBackendActionService(a) }
func BuildBackendSelection(a *App) backend.SelectionService { return buildBackendSelectionService(a) }
func BuildWorkspaceConfiguration(a *App) *workspacecmd.ConfigService {
	return buildWorkspaceConfigService(a)
}
func BuildWorkspaceManagement(a *App) *workspacecmd.ManagementService {
	return buildWorkspaceManagementService(a)
}
func BuildThreadMenu(a *App) *threadmenu.Service {
	return threadmenu.NewService(newThreadMenuDependencies(a))
}
