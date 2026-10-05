package feishuapp

import (
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
