package app

import (
	appcompact "feidex/internal/app/compact"
	"feidex/internal/feishu"
)

// ---------------------------------------------------------------------------
// *App methods satisfying compact.App
// ---------------------------------------------------------------------------

func (a *App) SessionStore() appcompact.SessionStore {
	if a == nil {
		return nil
	}
	return appState(a)
}

func (a *App) HandleBackendCompactCommand(msg *feishu.InboundMessage) error {
	return handleBackendCompactCommand(a, msg)
}

func (a *App) RunBackendCompactAction(sessionKey string, svc *appcompact.Service, action any) error {
	actions := backendActions(a)
	if actions == nil {
		return nil
	}
	var cardAction *feishu.CardAction
	if action != nil {
		cardAction, _ = action.(*feishu.CardAction)
	}
	return actions.runMenuCompactActionWithService(a, cardAction, sessionKey, svc)
}
