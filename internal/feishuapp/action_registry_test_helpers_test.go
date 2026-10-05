package feishuapp

import (
	"feidex/internal/adapter/feishu/goalcmd"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type cardActionService struct{ app *Frontend }

func newCardActionService(app *Frontend) cardActionDispatcher {
	return cardActionDispatcher{inner: app.bindings.CardActions}
}

func newMenuActionService(app *Frontend) cardActionService {
	return cardActionService{app: app}
}

func (s cardActionService) renderMenuNodeCard(actionName, sessionKey string) (map[string]any, bool) {
	return s.app.bindings.MenuCommands.fallback(actionName, sessionKey)
}

func (s cardActionService) completeMenuRoot(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return menuCoreCardActionHandlers(MenuCoreCardActionInputs{Backend: s.app.configView().configuredBackend, State: s.app.State(), Renderer: s.app.feishu})["menu.root"](action)
}

func (s cardActionService) completeMenuTools(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return menuCoreCardActionHandlers(MenuCoreCardActionInputs{Backend: s.app.configView().configuredBackend, State: s.app.State(), Renderer: s.app.feishu})["menu.tools"](action)
}

func (s cardActionService) completeMenuGroupModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.completeMenuModel(action, sessionKey)
}

func (s cardActionService) modelActionInputs() ModelCardActionInputs {
	return ModelCardActionInputs{
		Backend:                    s.app.configView().configuredBackend,
		BindingCommands:            s.app.bindings.BindingCommands,
		BackendConfiguration:       s.app.bindings.BackendConfiguration,
		ModelCommands:              s.app.bindings.ModelCommands,
		ModelSettings:              s.app.bindings.ModelSettings,
		ScopedRoutingConfiguration: s.app.bindings.ScopedRoutingConfiguration,
		ThreadSettings:             s.app.bindings.ThreadSettings,
		State:                      s.app.State(),
		CompleteMenuCommand:        s.app.bindings.MenuCommands.Complete,
	}
}

func (s cardActionService) completeMenuModel(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	setTestActionSessionKey(action, sessionKey)
	return modelCardActionHandlers(s.modelActionInputs())["menu.model"](action)
}

func (s cardActionService) completeMenuFast(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	setTestActionSessionKey(action, sessionKey)
	return modelCardActionHandlers(s.modelActionInputs())["menu.fast"](action)
}

func (s cardActionService) completeServiceTierSet(action *feishu.CardAction, sessionKey, threadID, serviceTier string) (*callback.CardActionTriggerResponse, error) {
	setTestActionSessionKey(action, sessionKey)
	action.ActionValue["thread_id"] = threadID
	action.ActionValue["service_tier"] = serviceTier
	return modelCardActionHandlers(s.modelActionInputs())["service_tier.set"](action)
}

func setTestActionSessionKey(action *feishu.CardAction, sessionKey string) {
	if action.ActionValue == nil {
		action.ActionValue = map[string]any{}
	}
	action.ActionValue["session_key"] = sessionKey
}

func (s cardActionService) completeMenuGroupSystem(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return menuCoreCardActionHandlers(MenuCoreCardActionInputs{Backend: s.app.configView().configuredBackend, State: s.app.State(), Renderer: s.app.feishu})["menu.group.system"](action)
}

func (s cardActionService) completeMenuStatus(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return systemCardActionHandlers(SystemCardActionInputs{CompleteMenuCommand: s.app.bindings.MenuCommands.Complete})["menu.status"](action)
}

func (s cardActionService) completeMenuHelp(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return systemCardActionHandlers(SystemCardActionInputs{CompleteMenuCommand: s.app.bindings.MenuCommands.Complete})["menu.help"](action)
}

func (s cardActionService) toolsActionInputs() ToolsCardActionInputs {
	return ToolsCardActionInputs{
		CompleteMenuCommand: s.app.bindings.MenuCommands.Complete,
		Backend:             s.app.configView().configuredBackend, State: s.app.State(), Renderer: s.app.feishu,
		RuntimeSettings: s.app.bindings.RuntimeSettings,
		QuietMode:       ConfiguredQuietModeBuilder(s.app.Config(), s.app.ConfigMu(), s.app.frontendConfigIndex),
	}
}

func (s cardActionService) completeMenuQuiet(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return toolsCardActionHandlers(s.toolsActionInputs())["menu.quiet"](action)
}

func (s cardActionService) completeQuietSet(action *feishu.CardAction, mode config.QuietMode) (*callback.CardActionTriggerResponse, error) {
	if action.ActionValue == nil {
		action.ActionValue = map[string]any{}
	}
	action.ActionValue["mode"] = mode.String()
	return toolsCardActionHandlers(s.toolsActionInputs())["quiet.set"](action)
}

func (s cardActionService) completeMenuHistory(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return toolsCardActionHandlers(s.toolsActionInputs())["menu.history"](action)
}

func (s cardActionService) completeMenuUsage(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return toolsCardActionHandlers(s.toolsActionInputs())["menu.usage"](action)
}

func completeMenuFork(a *Frontend, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if action.ActionValue == nil {
		action.ActionValue = map[string]any{}
	}
	action.ActionValue["session_key"] = sessionKey
	return threadForkCardActionHandlers(ThreadForkCardActionInputs{
		BindingCommands: a.bindings.BindingCommands, ConversationQuery: a.bindings.ConversationQuery,
		NormalizeSessionKey: a.configView().normalizeSessionKey,
		Backend:             a.configView().configuredBackend, CompleteMenuCommand: a.bindings.MenuCommands.Complete,
	})["menu.fork"](action)
}

func (s cardActionService) completeMenuReview(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if action.ActionValue == nil {
		action.ActionValue = map[string]any{}
	}
	action.ActionValue["session_key"] = sessionKey
	deps := s.app.bindings.ReviewCommand
	return reviewCardActionHandlers(ReviewCardActionInputs{
		Dependencies: deps, ReviewCommands: s.app.bindings.ReviewCommands,
		Backend: s.app.configView().configuredBackend, CompleteMenuCommand: s.app.bindings.MenuCommands.Complete,
	})["menu.review"](action)
}

func completeMenuPlanAsync(a *Frontend, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if action != nil {
		if action.ActionValue == nil {
			action.ActionValue = map[string]any{}
		}
		action.ActionValue["session_key"] = sessionKey
	}
	return planCardActionHandlers(PlanCardActionInputs{
		CompleteMenuCommand: a.bindings.MenuCommands.Complete,
		Lifecycle:           &a.runtimeOwner.Lifecycle,
		AsyncRunner:         a.asyncRunner,
		Context:             a.Context,
		FrontendID:          a.FrontendID(),
		EffectRunner:        newEffectRunner(a.runtimeOwner),
		State:               a.State(),
		ReplyInThread:       a.configView().replyInThreadEnabled(),
		TransportAvailable:  a.feishu != nil,
	})["menu.plan"](action)
}

func completeGoalRenderedActionAsync(a *Frontend, action *feishu.CardAction, sessionKey, toastText string, run func(goalcmd.Service) (*callback.CardActionTriggerResponse, error)) (*callback.CardActionTriggerResponse, error) {
	return runGoalCardActionAsync(GoalCardActionInputs{
		Commands:           *a.bindings.GoalCommands,
		Lifecycle:          &a.runtimeOwner.Lifecycle,
		AsyncRunner:        a.asyncRunner,
		Context:            a.Context,
		FrontendID:         a.FrontendID(),
		EffectRunner:       newEffectRunner(a.runtimeOwner),
		State:              a.State(),
		ReplyInThread:      a.configView().replyInThreadEnabled(),
		TransportAvailable: a.feishu != nil,
	}, action, sessionKey, toastText, "goal action patch failed", run)
}

func (s cardActionService) completeMenuCompact(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if action != nil {
		if action.ActionValue == nil {
			action.ActionValue = map[string]any{}
		}
		action.ActionValue["session_key"] = sessionKey
	}
	return compactCardActionHandlers(CompactCardActionInputs{
		CompleteMenuCommand: s.app.bindings.MenuCommands.Complete,
		Actions:             s.app.bindings.BackendActions,
		Compaction:          s.app.bindings.Compaction,
		State:               s.app.State(),
		Lifecycle:           &s.app.runtimeOwner.Lifecycle,
		AsyncRunner:         s.app.asyncRunner,
		Context:             s.app.Context(),
		FrontendID:          s.app.FrontendID(),
		EffectRunner:        newEffectRunner(s.app.runtimeOwner),
	})["menu.compact"](action)
}

func (s cardActionService) completeMenuUpgrade(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return maintenanceCardActionHandlers(MaintenanceCardActionInputs{
		Upgrades:        s.app.bindings.Upgrades,
		BackendUpgrades: s.app.bindings.BackendUpgrades,
		BackendActions:  s.app.bindings.BackendActions,
	})["menu.upgrade"](action)
}
