package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	appfeatures "feidex/internal/app/features"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type featureCommandBinding struct {
	Match    func(fields []string) bool
	Handle   func(a *App, msg *feishu.InboundMessage, args []string) error
	Backends map[string]func(fields []string) bool
}

type featureBinding struct {
	Commands      map[string]featureCommandBinding
	RenderActions []string
	Render        func(actionName string, a *App, sessionKey string) (map[string]any, bool)
	HandleAction  func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error)
}

var registryBackends = []string{backendCodex, backendClaude}

func buildFeatureBindings() map[string]featureBinding {
	return map[string]featureBinding{
		"menu.root": {
			Commands: map[string]featureCommandBinding{
				"menu": {
					Match: exactCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
						return sendCommandMenu(a, msg)
					},
				},
			},
			RenderActions: []string{"menu.root"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.root" {
					return nil, false
				}
				return renderCommandMenuCard(a, sessionKey), true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.root" {
					return nil, nil
				}
				return newMenuActionService(s.app).completeMenuRoot(action, actionSessionKey(action))
			},
		},
		"menu.tools": {
			RenderActions: []string{"menu.tools"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.tools" {
					return nil, false
				}
				return renderToolsMenuCard(a, sessionKey), true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.tools" {
					return nil, nil
				}
				return newMenuActionService(s.app).completeMenuTools(action, actionSessionKey(action))
			},
		},
		"menu.group.model": {
			RenderActions: []string{"menu.group.model"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.group.model" {
					return nil, false
				}
				return newBackendConfigurationService(a).renderModelMenuCard(sessionKey), true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.group.model" {
					return nil, nil
				}
				return newMenuActionService(s.app).completeMenuGroupModel(action, actionSessionKey(action))
			},
		},
		"menu.group.system": {
			RenderActions: []string{"menu.group.system"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.group.system" {
					return nil, false
				}
				return renderSystemMenuCard(a, sessionKey), true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.group.system" {
					return nil, nil
				}
				return newMenuActionService(s.app).completeMenuGroupSystem(action, actionSessionKey(action))
			},
		},
		"menu.group.backend": {
			Commands: map[string]featureCommandBinding{
				"backend": {
					Match: matchBackendCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newBackendSelectionService(a).commandBackend(msg, args)
					},
				},
			},
			RenderActions: []string{"menu.group.backend"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.group.backend" {
					return nil, false
				}
				return renderBackendMenuCard(a, sessionKey), true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				sessionKey := actionSessionKey(action)
				switch actionName {
				case "menu.group.backend":
					return newBackendSelectionService(s.app).completeMenuBackend(action, sessionKey)
				case "menu.backend", "menu.backend.switch":
					return newMenuActionService(s.app).completeMenuBackendSwitch(action, sessionKey)
				case "menu.auto_retry":
					return completeMenuCommand(s.app, action, sessionKey, "/backend retry", "menu.group.backend")
				case "backend.select":
					return newBackendSelectionService(s.app).completeBackendSelect(action, sessionKey, actionStringValue(action, "backend"))
				case "auto_retry.set":
					return newAutoRetryService(s.app).CompleteAutoRetrySet(action, strings.EqualFold(actionStringValue(action, "enabled"), "on"))
				default:
					return nil, nil
				}
			},
		},
		"menu.review": {
			Commands: map[string]featureCommandBinding{
				"review": {
					Match: matchReviewCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return commandReview(a, msg, args)
					},
					Backends: map[string]func(fields []string) bool{
						backendClaude: nil,
					},
				},
			},
			RenderActions: []string{"menu.review"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.review" {
					return nil, false
				}
				return newReviewFormService(a).renderReviewMenuCard(sessionKey), true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				sessionKey := actionSessionKey(action)
				switch actionName {
				case "menu.review":
					return newMenuActionService(s.app).completeMenuReview(action, sessionKey)
				case "menu.review.uncommitted":
					return completeMenuReviewUncommitted(s.app, action, sessionKey)
				case "menu.review.base":
					return completeMenuReviewBase(s.app, action, sessionKey)
				case "menu.review.commit":
					return completeMenuReviewCommit(s.app, action, sessionKey)
				case "menu.review.custom":
					return completeMenuCommand(s.app, action, sessionKey, "/review custom", "menu.review")
				default:
					return nil, nil
				}
			},
		},
		"menu.quiet": {
			Commands: map[string]featureCommandBinding{
				"quiet": {
					Match: func(fields []string) bool {
						return exactOrSingleArgCommand(fields, "config", "verbose", "progress", "normal", "final")
					},
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return commandQuiet(a, msg, args)
					},
				},
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				switch actionName {
				case "menu.quiet":
					return newMenuActionService(s.app).completeMenuQuiet(action, actionSessionKey(action))
				case "quiet.set":
					return newMenuActionService(s.app).completeQuietSet(action, config.QuietMode(actionStringValue(action, "mode")))
				default:
					return nil, nil
				}
			},
		},
		"menu.compact": {
			Commands: map[string]featureCommandBinding{
				"compact": {
					Match: exactCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return commandCompact(a, msg, args)
					},
				},
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.compact" {
					return nil, nil
				}
				return newMenuActionService(s.app).completeMenuCompact(action, actionSessionKey(action))
			},
		},
		"menu.download": {
			Commands: map[string]featureCommandBinding{
				"download": {
					Match: exactCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return commandDownload(a, msg, args)
					},
				},
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.download" {
					return nil, nil
				}
				return completeMenuDownload(s.app, action, actionSessionKey(action))
			},
		},
		"menu.history": {
			Commands: map[string]featureCommandBinding{
				"history": {
					Match: matchHistoryCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newHistoryService(a).CommandHistory(msg, args)
					},
				},
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				sessionKey := actionSessionKey(action)
				switch actionName {
				case "menu.history":
					return newMenuActionService(s.app).completeMenuHistory(action, sessionKey)
				case "history.page":
					return newMenuActionService(s.app).completeHistoryPage(action, sessionKey, actionIntValue(action, "page"))
				case "history.detail":
					return newMenuActionService(s.app).completeHistoryDetail(action, sessionKey, actionIntValue(action, "index"))
				case "history.detail.select":
					errResp, index, ok := actionIndexOption(action, "未收到有效 turn 选项")
					if !ok {
						return errResp, nil
					}
					return newMenuActionService(s.app).completeHistoryDetail(action, sessionKey, index)
				default:
					return nil, nil
				}
			},
		},
		"menu.skills": {
			Commands: map[string]featureCommandBinding{
				"skills": {
					Match: matchSkillsCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newSkillsService(a).CommandSkills(msg, args)
					},
					Backends: map[string]func(fields []string) bool{
						backendClaude: nil,
					},
				},
			},
			RenderActions: []string{"menu.skills"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.skills" {
					return nil, false
				}
				card, err := newSkillsService(a).RenderSkillsCard(sessionKey, false)
				if err != nil {
					return nil, false
				}
				return card, true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				sessionKey := actionSessionKey(action)
				switch actionName {
				case "menu.skills":
					return newMenuActionService(s.app).completeMenuSkills(action, sessionKey)
				case "skills.select":
					return newSkillsService(s.app).CompleteSkillsSelect(action, sessionKey, strings.TrimSpace(action.Option))
				case "skills.reload":
					return newSkillsService(s.app).CompleteSkillsReload(action, sessionKey)
				default:
					return nil, nil
				}
			},
		},
		"menu.usage": {
			Commands: map[string]featureCommandBinding{
				"usage": {
					Match: exactCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newUsageService(a).CommandUsage(msg, args)
					},
				},
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.usage" {
					return nil, nil
				}
				return newMenuActionService(s.app).completeMenuUsage(action, actionSessionKey(action))
			},
		},
		"menu.interrupt": {
			Commands: map[string]featureCommandBinding{
				"interrupt": {
					Match: exactCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
						return commandInterrupt(a, msg)
					},
				},
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.interrupt" {
					return nil, nil
				}
				return newThreadService(s.app).CompleteMenuInterrupt(action, actionSessionKey(action), actionStringValue(action, "turn_id"))
			},
		},
		"menu.thread": {
			Commands: map[string]featureCommandBinding{
				"fork": {
					Match: exactCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return commandFork(a, msg, args)
					},
				},
				"new": {
					Match: exactCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
						return newThreadService(a).CommandThreadsNew(msg)
					},
				},
				"thread": {
					Match: matchThreadCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newThreadService(a).CommandThread(msg, args)
					},
					Backends: map[string]func(fields []string) bool{
						backendClaude: nil,
					},
				},
				"session": {
					Match: matchSessionCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newThreadService(a).CommandSession(msg, args)
					},
					Backends: map[string]func(fields []string) bool{
						backendCodex: nil,
					},
				},
				"threads": {
					Match: exactCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						if len(args) > 0 {
							return fmt.Errorf("usage: /threads")
						}
						return newThreadService(a).CommandThread(msg, []string{"list"})
					},
					Backends: map[string]func(fields []string) bool{
						backendClaude: nil,
					},
				},
			},
			RenderActions: []string{"menu.thread"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.thread" {
					return nil, false
				}
				card, err := conversationBackend(a).RenderThreadsCard(sessionKey, false)
				if err != nil {
					return nil, false
				}
				return card, true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				sessionKey := actionSessionKey(action)
				switch actionName {
				case "menu.thread":
					return newThreadService(s.app).CompleteMenuThread(action, sessionKey)
				case "menu.new":
					return newThreadService(s.app).CompleteMenuNew(action, sessionKey)
				case "menu.fork":
					return completeMenuFork(s.app, action, sessionKey)
				default:
					return nil, nil
				}
			},
		},
		"menu.workspace": {
			Commands: map[string]featureCommandBinding{
				"workspace": {
					Match: matchWorkspaceCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newWorkspaceService(a).commandWorkspace(msg, args)
					},
					Backends: map[string]func(fields []string) bool{
						backendClaude: matchClaudeWorkspaceCommand,
					},
				},
			},
			RenderActions: []string{"menu.workspace"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.workspace" {
					return nil, false
				}
				return newWorkspaceConfigService(a).renderWorkspaceMenuCard(sessionKey), true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.workspace" {
					return nil, nil
				}
				return completeMenuWorkspace(s.app, action, actionSessionKey(action))
			},
		},
		"menu.model": {
			Commands: map[string]featureCommandBinding{
				"model": {
					Match: matchModelCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newBackendConfigurationService(a).handleBackendModelCommand(msg, args)
					},
				},
				"effort": {
					Match: matchEffortCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newModelConfigService(a).commandEffort(msg, args)
					},
				},
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				switch actionName {
				case "menu.model":
					return newMenuActionService(s.app).completeMenuModel(action, actionSessionKey(action))
				case "model.config.set_model":
					return newBackendConfigurationService(s.app).completeGlobalModelSet(action, actionStringValue(action, "model_id"))
				case "model.config.select_model":
					modelID := strings.TrimSpace(action.Option)
					if modelID == modelConfigDefaultOptionValue {
						modelID = ""
					}
					return newBackendConfigurationService(s.app).completeGlobalModelSet(action, modelID)
				case "model.config.set_effort":
					return newBackendConfigurationService(s.app).completeGlobalReasoningEffortSet(action, actionStringValue(action, "reasoning_effort"))
				case "model.config.select_effort":
					reasoningEffort := strings.TrimSpace(action.Option)
					if reasoningEffort == modelConfigDefaultOptionValue {
						reasoningEffort = ""
					}
					return newBackendConfigurationService(s.app).completeGlobalReasoningEffortSet(action, reasoningEffort)
				default:
					return nil, nil
				}
			},
		},
		"menu.fast": {
			Commands: map[string]featureCommandBinding{
				"fast": {
					Match: func(fields []string) bool {
						return exactOrSingleArgCommand(fields, "config", "fast", "default", "off", "toggle")
					},
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return commandFast(a, msg, args)
					},
					Backends: map[string]func(fields []string) bool{
						backendClaude: nil,
					},
				},
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				switch actionName {
				case "menu.fast":
					return newMenuActionService(s.app).completeMenuFast(action, actionSessionKey(action))
				case "service_tier.set":
					return newMenuActionService(s.app).completeServiceTierSet(action, actionSessionKey(action), actionStringValue(action, "thread_id"), actionStringValue(action, "service_tier"))
				default:
					return nil, nil
				}
			},
		},
		"menu.debug": {
			Commands: map[string]featureCommandBinding{
				"debug": {
					Match: func(fields []string) bool {
						return exactOrSingleArgCommand(fields, "on", "off", "logs")
					},
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newDebugService(a).CommandDebug(msg, args)
					},
				},
			},
			RenderActions: []string{"menu.debug.logs"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.debug.logs" {
					return nil, false
				}
				return newDebugService(a).RenderDebugLogsCard(sessionKey), true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				sessionKey := actionSessionKey(action)
				switch actionName {
				case "menu.debug":
					return newDebugService(s.app).CompleteMenuDebug(action, sessionKey)
				case "menu.debug.logs":
					return newDebugService(s.app).CompleteMenuDebugLogs(action, sessionKey)
				default:
					return nil, nil
				}
			},
		},
		"menu.status": {
			Commands: map[string]featureCommandBinding{
				"status": {
					Match: exactCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
						return commandStatus(a, msg)
					},
				},
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.status" {
					return nil, nil
				}
				return newMenuActionService(s.app).completeMenuStatus(action, actionSessionKey(action))
			},
		},
		"menu.help": {
			Commands: map[string]featureCommandBinding{
				"help": {
					Match: exactCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return commandHelp(a, msg, args)
					},
				},
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.help" {
					return nil, nil
				}
				return newMenuActionService(s.app).completeMenuHelp(action, actionSessionKey(action))
			},
		},
		"menu.codex_upgrade": {
			Commands: map[string]featureCommandBinding{
				"codex": {
					Match: matchCodexCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newBackendUpgradeService(a).commandCodex(msg, args)
					},
				},
			},
			RenderActions: []string{"menu.codex_upgrade"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.codex_upgrade" {
					return nil, false
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				view, err := newBackendUpgradeService(a).loadCodexUpgradeView(ctx, false)
				if err != nil {
					return nil, false
				}
				return newUpgradeRenderService(a).renderCodexUpgradeStatusCard(sessionKey, view, false), true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.codex_upgrade" {
					return nil, nil
				}
				return newBackendUpgradeService(s.app).completeMenuCodexUpgrade(action)
			},
		},
		"menu.claude_upgrade": {
			Commands: map[string]featureCommandBinding{
				"claude": {
					Match: matchClaudeCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newBackendUpgradeService(a).commandClaude(msg, args)
					},
				},
			},
			RenderActions: []string{"menu.claude_upgrade"},
			Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
				if actionName != "menu.claude_upgrade" {
					return nil, false
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				view, err := newBackendUpgradeService(a).loadClaudeUpgradeView(ctx, false)
				if err != nil {
					return nil, false
				}
				return newUpgradeRenderService(a).renderClaudeUpgradeStatusCard(sessionKey, view, false), true
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.claude_upgrade" {
					return nil, nil
				}
				return newBackendUpgradeService(s.app).completeMenuClaudeUpgrade(action)
			},
		},
		"menu.upgrade": {
			Commands: map[string]featureCommandBinding{
				"upgrade": {
					Match: matchUpgradeCommand,
					Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
						return newAppUpgradeService(a).commandUpgrade(msg, args)
					},
				},
			},
			HandleAction: func(actionName string, s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
				if actionName != "menu.upgrade" {
					return nil, nil
				}
				return newMenuActionService(s.app).completeMenuUpgrade(action)
			},
		},
	}
}

func featureBindingForID(id string) (featureBinding, bool) {
	binding, ok := featureBindingsRegistry()[id]
	return binding, ok
}

var (
	featureBindingsOnce          sync.Once
	cachedFeatureBindings        map[string]featureBinding
	localCommandSpecsOnce        sync.Once
	cachedLocalCommandSpecs      []localCommandSpec
	menuNodeRenderersOnce        sync.Once
	cachedMenuNodeRenderers      map[string]menuNodeRenderer
	menuCardActionHandlersOnce   sync.Once
	cachedMenuCardActionHandlers map[string]cardActionHandler
)

func featureBindingsRegistry() map[string]featureBinding {
	featureBindingsOnce.Do(func() {
		cachedFeatureBindings = buildFeatureBindings()
	})
	return cachedFeatureBindings
}

func localCommandSpecsRegistry() []localCommandSpec {
	localCommandSpecsOnce.Do(func() {
		cachedLocalCommandSpecs = buildLocalCommandSpecs()
	})
	return append([]localCommandSpec(nil), cachedLocalCommandSpecs...)
}

func menuNodeRenderersRegistry() map[string]menuNodeRenderer {
	menuNodeRenderersOnce.Do(func() {
		cachedMenuNodeRenderers = buildMenuNodeRenderers()
	})
	return cachedMenuNodeRenderers
}

func menuCardActionHandlersRegistry() map[string]cardActionHandler {
	menuCardActionHandlersOnce.Do(func() {
		cachedMenuCardActionHandlers = buildMenuCardActionHandlers()
	})
	return cachedMenuCardActionHandlers
}

func buildLocalCommandSpecs() []localCommandSpec {
	specs := make([]localCommandSpec, 0, 32)
	for _, feature := range appfeatures.All() {
		binding, ok := featureBindingForID(feature.ID)
		if !ok {
			if len(feature.Commands) > 0 {
				panic("missing feature command binding for " + feature.ID)
			}
			continue
		}
		for _, command := range feature.Commands {
			commandBinding, ok := binding.Commands[command.ID]
			if !ok {
				panic("missing command binding for feature " + feature.ID + " command " + command.ID)
			}
			spec := localCommandSpec{
				Names:       append([]string(nil), command.Names...),
				IsLocal:     commandBinding.Match,
				Handle:      commandBinding.Handle,
				HelpGroup:   strings.TrimSpace(command.HelpGroup),
				HelpEntries: append([]helpCommandSpec(nil), command.HelpEntries...),
				Backends:    buildLocalCommandBackendPolicies(feature, command, commandBinding),
			}
			specs = append(specs, spec)
		}
	}
	return specs
}

func buildLocalCommandBackendPolicies(feature appfeatures.Spec, command appfeatures.CommandSpec, binding featureCommandBinding) map[string]localCommandBackendSpec {
	policies := map[string]localCommandBackendSpec{}
	for _, backend := range registryBackends {
		metaPolicy, hasMetaPolicy := command.Backends[backend]
		match := binding.Match
		hasBindingPolicy := false
		if binding.Backends != nil {
			if backendMatch, ok := binding.Backends[backend]; ok {
				match = backendMatch
				hasBindingPolicy = true
			}
		}
		if !feature.SupportsBackend(backend) {
			match = nil
			metaPolicy.HideInHelp = true
			hasMetaPolicy = true
			hasBindingPolicy = true
		}
		if !hasMetaPolicy && !hasBindingPolicy {
			continue
		}
		policies[backend] = localCommandBackendSpec{
			Match:       match,
			HideInHelp:  metaPolicy.HideInHelp,
			HelpEntries: append([]helpCommandSpec(nil), metaPolicy.HelpEntries...),
		}
	}
	if len(policies) == 0 {
		return nil
	}
	return policies
}

func buildMenuNodeRenderers() map[string]menuNodeRenderer {
	renderers := map[string]menuNodeRenderer{}
	for _, feature := range appfeatures.All() {
		binding, ok := featureBindingForID(feature.ID)
		if !ok || binding.Render == nil {
			continue
		}
		for _, actionName := range binding.RenderActions {
			name := strings.TrimSpace(actionName)
			if name == "" {
				continue
			}
			renderers[name] = func(actionName string, binding featureBinding) menuNodeRenderer {
				return func(a *App, sessionKey string) (map[string]any, bool) {
					return binding.Render(actionName, a, sessionKey)
				}
			}(name, binding)
		}
	}
	return renderers
}

func buildMenuCardActionHandlers() map[string]cardActionHandler {
	handlers := map[string]cardActionHandler{}
	for _, feature := range appfeatures.All() {
		if len(feature.ActionNames) == 0 {
			continue
		}
		binding, ok := featureBindingForID(feature.ID)
		if !ok || binding.HandleAction == nil {
			panic("missing feature action binding for " + feature.ID)
		}
		for _, actionName := range feature.ActionNames {
			name := strings.TrimSpace(actionName)
			if name == "" {
				continue
			}
			handlers[name] = func(actionName string, binding featureBinding) cardActionHandler {
				return func(s cardActionService, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
					return binding.HandleAction(actionName, s, action)
				}
			}(name, binding)
		}
	}
	return handlers
}
