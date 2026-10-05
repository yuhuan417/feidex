package feishuapp

import (
	"fmt"

	"feidex/internal/feishu"
)

func appendFeatureBindingsThreadWorkspace(bindings map[string]featureBinding) {
	bindings["menu.interrupt"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"interrupt": {
				Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
					return a.bindings.ThreadMenu.CommandInterrupt(msg)
				},
			},
		},
		PortActions: []string{"menu.interrupt"},
	}
	bindings["menu.thread"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"fork": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.ThreadMenu.CommandFork(msg, args)
				},
			},
			"new": {
				Handle: func(a *App, msg *feishu.InboundMessage, _ []string) error {
					return a.bindings.ThreadMenu.CommandThreadsNew(msg)
				},
			},
			"thread": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.ThreadMenu.CommandThread(msg, args)
				},
			},
			"session": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					return a.bindings.ThreadMenu.CommandSession(msg, args)
				},
			},
			"threads": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if len(args) > 0 {
						return fmt.Errorf("usage: /threads")
					}
					return a.bindings.ThreadMenu.CommandThread(msg, []string{"list"})
				},
			},
		},
		RenderActions: []string{"menu.thread"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.thread" {
				return nil, false
			}
			sessionKey = threadMenuEffectiveSessionKey(a.configView().normalizeSessionKey, a.bindings.BindingCommands.scope, a.bindings.ConversationQuery, sessionKey)
			backend := ConfiguredBackendBuilder(a.Config(), a.ConfigMu(), a.runtimeOwner.Backend, a.FrontendID(), a.frontendConfigIndex)
			card, err := renderThreadsCard(threadCardInputs{
				Repository: a.State(), Config: a.Config(), Backend: backend, Conversations: a.bindings.Conversations,
			}, sessionKey, false)
			if err != nil {
				return nil, false
			}
			return card, true
		},
		PortActions: []string{"menu.thread", "menu.new", "menu.fork"},
	}
	bindings["menu.workspace"] = featureBinding{
		Commands: map[string]featureCommandBinding{
			"workspace": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if groupBindingScopeActive(msg) {
						return a.bindings.BindingCommands.commandWorkspace(msg, args)
					}
					return commandWorkspace(a, msg, args)
				},
			},
		},
		RenderActions: []string{"menu.workspace"},
		Render: func(actionName string, a *App, sessionKey string) (map[string]any, bool) {
			if actionName != "menu.workspace" {
				return nil, false
			}
			return a.bindings.WorkspacePresentation.RenderWorkspaceMenuCard(sessionKey), true
		},
		PortActions: []string{"menu.workspace"},
	}
	bindings["menu.model"] = featureBinding{
		PortActions: []string{"menu.model", "menu.model_auxiliary", "model.config.set_model", "model.config.select_model", "model.config.add_option", "model.config.remove_option", "model.config.set_effort", "model.config.select_effort", "model.plan_config.set_model", "model.plan_config.select_model", "model.plan_config.set_effort", "model.plan_config.select_effort", "model.aux_config.select_review_model", "model.aux_config.select_plan_model", "model.aux_config.select_plan_effort", "model.aux_config.select_subagent_model", "model.aux_config.select_subagent_effort", "model.aux_config.select_small_model"},
		Commands: map[string]featureCommandBinding{
			"model": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if groupBindingScopeActive(msg) {
						return a.bindings.BindingCommands.commandModel(msg, args)
					}
					return commandModelProfileAware(a, msg, args)
				},
			},
			"effort": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if groupBindingScopeActive(msg) {
						return a.bindings.BindingCommands.commandEffort(msg, args)
					}
					return commandEffortProfileAware(a.bindings.ModelCommands, msg, args)
				},
			},
		},
	}
	bindings["menu.fast"] = featureBinding{
		PortActions: []string{"menu.fast", "service_tier.set"},
		Commands: map[string]featureCommandBinding{
			"fast": {
				Handle: func(a *App, msg *feishu.InboundMessage, args []string) error {
					if groupBindingScopeActive(msg) {
						return a.bindings.BindingCommands.commandFast(msg, args)
					}
					return commandFastProfileAware(a, msg, args)
				},
			},
		},
	}
}
