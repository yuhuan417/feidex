package feishuapp

import (
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newCardActionService(app *App) cardActionDispatcher {
	return cardActionDispatcher{inner: app.bindings.CardActions}
}

func newMenuActionService(app *App) cardActionService {
	return cardActionService{app: app}
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

func (s cardActionService) completeMenuGroupSystem(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return menuCoreCardActionHandlers(MenuCoreCardActionInputs{Backend: s.app.configView().configuredBackend, State: s.app.State(), Renderer: s.app.feishu})["menu.group.system"](action)
}

func (s cardActionService) completeMenuStatus(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return systemCardActionHandlers(SystemCardActionInputs{CompleteMenuCommand: s.app.CompleteMenuCommand})["menu.status"](action)
}

func (s cardActionService) completeMenuHelp(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return systemCardActionHandlers(SystemCardActionInputs{CompleteMenuCommand: s.app.CompleteMenuCommand})["menu.help"](action)
}

func (s cardActionService) toolsActionInputs() ToolsCardActionInputs {
	return ToolsCardActionInputs{
		CompleteMenuCommand: s.app.CompleteMenuCommand,
		Backend:             s.app.configView().configuredBackend, State: s.app.State(), Renderer: s.app.feishu,
		RuntimeSettings: s.app.bindings.RuntimeSettings,
		QuietMode:       ConfiguredQuietModeBuilder(s.app.Config(), s.app.ConfigMu(), s.app.FrontendConfigIndex()),
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

func completeMenuFork(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if action.ActionValue == nil {
		action.ActionValue = map[string]any{}
	}
	action.ActionValue["session_key"] = sessionKey
	return threadForkCardActionHandlers(ThreadForkCardActionInputs{
		BindingCommands: a.bindings.BindingCommands, ConversationQuery: a.bindings.ConversationQuery,
		NormalizeSessionKey: a.configView().normalizeSessionKey,
		Backend:             a.configView().configuredBackend, CompleteMenuCommand: a.CompleteMenuCommand,
	})["menu.fork"](action)
}

func (s cardActionService) completeMenuReview(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if action.ActionValue == nil {
		action.ActionValue = map[string]any{}
	}
	action.ActionValue["session_key"] = sessionKey
	deps := ReviewCommandDependencies(s.app)
	return reviewCardActionHandlers(ReviewCardActionInputs{
		Dependencies: deps, ReviewCommands: s.app.bindings.ReviewCommands,
		Backend: s.app.configView().configuredBackend, CompleteMenuCommand: s.app.CompleteMenuCommand,
	})["menu.review"](action)
}
