package feishuapp

import (
	"context"
	"fmt"
	"time"

	appbackend "feidex/internal/adapter/feishu/backend"
	appdebugviewcmd "feidex/internal/adapter/feishu/debugviewcmd"
	"feidex/internal/adapter/feishu/goalcmd"
	"feidex/internal/adapter/feishu/history"
	modelcommands "feidex/internal/adapter/feishu/modelconfig"
	"feidex/internal/adapter/feishu/planmode"
	appreviewcmd "feidex/internal/adapter/feishu/reviewcmd"
	tier "feidex/internal/adapter/feishu/servicetier"
	"feidex/internal/adapter/feishu/skills"
	"feidex/internal/adapter/feishu/threadmenu"
	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
	"feidex/internal/adapter/feishu/upgraderender"
	workspacecards "feidex/internal/adapter/feishu/workspace"
	"feidex/internal/adapter/feishu/workspacecmd"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/compaction"
	"feidex/internal/application/conversation"
	"feidex/internal/application/modelconfig"
	"feidex/internal/application/runtimeconfig"
	"feidex/internal/compositionkit"
	"feidex/internal/config"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
)

type CommandFeatureDependencies struct {
	BindingScope               BindingScope
	BindingCommands            bindingService
	BackendSelection           appbackend.SelectionService
	BackendConfiguration       appbackend.ConfigurationService
	ReviewCommands             appreviewcmd.ReviewFormService
	RuntimeSettings            runtimeconfig.Service
	QuietMode                  func() config.QuietMode
	PlanMode                   planmode.Dependencies
	GoalCommands               *goalcmd.Service
	BackendActions             appbackend.ActionService
	Compaction                 *compaction.Service
	Download                   func(*feishu.InboundMessage, []string) error
	History                    history.Service
	SkillCommands              *skills.Service
	Usage                      appdebugviewcmd.UsageService
	ThreadMenu                 *threadmenu.Service
	WorkspaceConfiguration     *workspacecmd.ConfigService
	WorkspaceManagement        *workspacecmd.ManagementService
	WorkspacePresentation      *workspacecards.Presentation
	ModelCommands              modelcommands.ModelConfigService
	ModelSettings              modelconfig.SettingsService
	ScopedRoutingConfiguration compositionkit.ScopedRoutingConfiguration
	ServiceTier                tier.Service
	Debug                      appdebugviewcmd.DebugService
	BackendUpgrades            backendUpgradeService
	UpgradePresentation        upgradeRenderService
	Upgrades                   appupgradecmd.UpgradeService
	State                      *appstate.Store
	Config                     *config.Config
	ConfiguredBackend          func() string
	MakeSessionKey             func(*feishu.InboundMessage) string
	NormalizeSessionKey        func(string) string
	ConversationQuery          conversation.Query
	Conversations              *conversation.Service
	Renderer                   FeishuClient
	Effects                    frontendruntime.EffectRunner
	FrontendID                 string
	ReplyInThread              bool
}

func BuildCommandFeatureInputs(inputs CommandFeatureDependencies) CommandFeatureInputs {
	quiet := QuietCommandInputs{
		Settings: inputs.RuntimeSettings, Mode: inputs.QuietMode, MakeSessionKey: inputs.MakeSessionKey,
		State: inputs.State, Renderer: inputs.Renderer, Effects: inputs.Effects,
		FrontendID: inputs.FrontendID, ReplyInThread: inputs.ReplyInThread,
	}
	profile := ProfileCommandInputs{
		BackendConfiguration: inputs.BackendConfiguration, ModelSettings: inputs.ModelSettings,
		ScopedRoutingConfiguration: inputs.ScopedRoutingConfiguration, ServiceTier: inputs.ServiceTier,
		ConfiguredBackend: inputs.ConfiguredBackend, MakeSessionKey: inputs.MakeSessionKey,
		Effects: inputs.Effects, FrontendID: inputs.FrontendID, ReplyInThread: inputs.ReplyInThread,
	}
	title := func(sessionKey, fallback string) string {
		return planModeTitleForSession(inputs.State, inputs.State != nil, sessionKey, fallback)
	}
	return CommandFeatureInputs{
		Menu: func(msg *feishu.InboundMessage, _ []string) error {
			return sendCommandMenuWith(inputs.MakeSessionKey, inputs.ConfiguredBackend, inputs.State, inputs.Renderer, inputs.Effects, inputs.FrontendID, inputs.ReplyInThread, msg)
		},
		Backend: inputs.BackendSelection.CommandBackend,
		Primary: inputs.BindingCommands.commandPrimary,
		Review:  inputs.ReviewCommands.CommandReview,
		Quiet: func(msg *feishu.InboundMessage, args []string) error {
			return handleQuietCommand(quiet, msg, args)
		},
		Plan: func(msg *feishu.InboundMessage, args []string) error {
			return planmode.CommandPlan(inputs.PlanMode, msg, args)
		},
		Goal: func(msg *feishu.InboundMessage, raw string, args []string) error {
			return commandGoalRaw(inputs.GoalCommands, msg, raw, args)
		},
		Compact: func(msg *feishu.InboundMessage, args []string) error {
			return commandCompact(inputs.BackendActions, inputs.Compaction, msg, args)
		},
		Download: func(msg *feishu.InboundMessage, args []string) error {
			if inputs.Download == nil {
				return nil
			}
			return inputs.Download(msg, args)
		},
		History: inputs.History.CommandHistory,
		Skills:  inputs.SkillCommands.CommandSkills,
		Usage:   inputs.Usage.CommandUsage,
		Interrupt: func(msg *feishu.InboundMessage, _ []string) error {
			return inputs.ThreadMenu.CommandInterrupt(msg)
		},
		Fork: inputs.ThreadMenu.CommandFork,
		New: func(msg *feishu.InboundMessage, _ []string) error {
			return inputs.ThreadMenu.CommandThreadsNew(msg)
		},
		Thread:  inputs.ThreadMenu.CommandThread,
		Session: inputs.ThreadMenu.CommandSession,
		Threads: func(msg *feishu.InboundMessage, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("usage: /threads")
			}
			return inputs.ThreadMenu.CommandThread(msg, []string{"list"})
		},
		Workspace: func(msg *feishu.InboundMessage, args []string) error {
			if groupBindingScopeActive(msg) {
				return inputs.BindingCommands.commandWorkspace(msg, args)
			}
			return inputs.WorkspaceConfiguration.CommandWorkspace(msg, args, inputs.WorkspaceManagement)
		},
		Model: func(msg *feishu.InboundMessage, args []string) error {
			if groupBindingScopeActive(msg) {
				return inputs.BindingCommands.commandModel(msg, args)
			}
			return handleModelProfileCommand(profile, msg, args)
		},
		Effort: func(msg *feishu.InboundMessage, args []string) error {
			if groupBindingScopeActive(msg) {
				return inputs.BindingCommands.commandEffort(msg, args)
			}
			return commandEffortProfileAware(inputs.ModelCommands, msg, args)
		},
		Fast: func(msg *feishu.InboundMessage, args []string) error {
			if groupBindingScopeActive(msg) {
				return inputs.BindingCommands.commandFast(msg, args)
			}
			return handleFastProfileCommand(profile, msg, args)
		},
		Debug: inputs.Debug.CommandDebug,
		Status: func(msg *feishu.InboundMessage, _ []string) error {
			return handleStatusCommand(inputs.State, inputs.ConfiguredBackend, inputs.BackendConfiguration, inputs.MakeSessionKey, inputs.Renderer, inputs.Effects, inputs.FrontendID, inputs.ReplyInThread, msg)
		},
		Help: func(msg *feishu.InboundMessage, args []string) error {
			return handleHelpCommand(inputs.BindingScope.scope, inputs.ConfiguredBackend, inputs.MakeSessionKey, inputs.State, inputs.Effects, inputs.FrontendID, inputs.ReplyInThread, msg, args)
		},
		Codex:   inputs.BackendUpgrades.commandCodex,
		Claude:  inputs.BackendUpgrades.commandClaude,
		Upgrade: inputs.Upgrades.CommandUpgrade,
		MenuRoot: func(sessionKey string) (map[string]any, bool) {
			return renderCommandMenuCardData(inputs.ConfiguredBackend(), title(sessionKey, "主菜单"), inputs.Renderer, sessionKey), true
		},
		MenuTools: func(sessionKey string) (map[string]any, bool) {
			spec, _ := menuGroupSpec("menu.tools")
			return renderToolsMenuCardData(inputs.ConfiguredBackend(), title(sessionKey, spec.Label), inputs.Renderer, sessionKey), true
		},
		MenuGroupModel: func(sessionKey string) (map[string]any, bool) {
			// The root button opens /model directly. If that command fails, return
			// to /menu instead of reviving the removed model overview card.
			return renderCommandMenuCardData(inputs.ConfiguredBackend(), title(sessionKey, "主菜单"), inputs.Renderer, sessionKey), true
		},
		MenuGroupSystem: func(sessionKey string) (map[string]any, bool) {
			spec, _ := menuGroupSpec("menu.group.system")
			return renderSystemMenuCardData(inputs.ConfiguredBackend(), title(sessionKey, spec.Label), inputs.Renderer, sessionKey), true
		},
		MenuGroupBackend: func(sessionKey string) (map[string]any, bool) {
			spec, _ := menuGroupSpec("menu.group.backend")
			return renderBackendMenuCardData(inputs.ConfiguredBackend(), title(sessionKey, spec.Label), inputs.Renderer, sessionKey), true
		},
		MenuCurrentBot: func(sessionKey string) (map[string]any, bool) {
			return renderCommandMenuCardData(inputs.ConfiguredBackend(), title(sessionKey, "主菜单"), inputs.Renderer, sessionKey), true
		},
		MenuCurrentWorkspace: func(sessionKey string) (map[string]any, bool) {
			return inputs.WorkspacePresentation.RenderWorkspaceMenuCard(sessionKey), true
		},
		MenuThread: func(sessionKey string) (map[string]any, bool) {
			sessionKey = threadMenuEffectiveSessionKey(inputs.NormalizeSessionKey, inputs.BindingScope.scope, inputs.ConversationQuery, sessionKey)
			card, err := renderThreadsCard(threadCardInputs{
				Repository: inputs.State, Config: inputs.Config, Backend: inputs.ConfiguredBackend, Conversations: inputs.Conversations,
			}, sessionKey, false)
			return card, err == nil
		},
		MenuWorkspace: func(sessionKey string) (map[string]any, bool) {
			return inputs.WorkspacePresentation.RenderWorkspaceMenuCard(sessionKey), true
		},
		MenuReview: func(sessionKey string) (map[string]any, bool) {
			return inputs.ReviewCommands.RenderReviewMenuCard(sessionKey), true
		},
		MenuSkills: func(sessionKey string) (map[string]any, bool) {
			card, err := inputs.SkillCommands.RenderSkillsCard(sessionKey, false)
			return card, err == nil
		},
		MenuDebugLogs: func(sessionKey string) (map[string]any, bool) {
			return inputs.Debug.RenderDebugLogsCard(sessionKey), true
		},
		MenuCodexUpgrade:  upgradeMenuRenderer(inputs.BackendUpgrades.loadCodexUpgradeView, inputs.UpgradePresentation, upgraderender.CodexSpec),
		MenuClaudeUpgrade: upgradeMenuRenderer(inputs.BackendUpgrades.loadClaudeUpgradeView, inputs.UpgradePresentation, upgraderender.ClaudeSpec),
	}
}

func upgradeMenuRenderer(load func(context.Context, bool) (upgraderender.UpgradeView, error), presentation upgradeRenderService, spec upgraderender.Spec) MenuNodeRenderer {
	return func(sessionKey string) (map[string]any, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		view, err := load(ctx, false)
		if err != nil {
			return nil, false
		}
		return presentation.renderUpgradeStatusCard(spec, sessionKey, view, false), true
	}
}
